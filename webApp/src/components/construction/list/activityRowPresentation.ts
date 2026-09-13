/**
 * The LIST lens's PRESENTATION rules — which channel carries which fact.
 *
 * Two prototype rounds were rejected for visual density before this design, and
 * the diagnosis that came out of them is the one rule this module encodes:
 *
 *     every magnitude gets a geometric channel;
 *     nothing with a geometry gets a chip.
 *
 * A state is a category, not a magnitude, so a chip is legitimate there — but at
 * most ONE per row, at the tier that owns it, and `unknown` gets none at all:
 * chip-less IS the signal, and unknown is the MAJORITY state (292 of the 384
 * task rows in the committed project). A screen of "UNKNOWN" chips is precisely
 * the density failure that got the earlier rounds rejected.
 *
 * Everything here is pure — no React, no DOM, no tokens resolved. It is a plain
 * `.ts` sibling of ActivityTreeView.tsx for the reason activityTree.ts and
 * detailPaneState.ts already are: Node's native type-stripping test runner
 * cannot load a `.tsx` module at all (`ERR_UNKNOWN_FILE_EXTENSION`), so the
 * rules the brief's tests exercise must live outside the renderer. Relative
 * VALUE imports therefore carry an explicit `.ts` extension; type-only imports
 * are erased before Node ever sees them and need none.
 *
 * Colour is NOT decided here. The two token-driven palettes this surface uses
 * already exist (detailPaneState.taskDetailStateFill for the state vocabulary,
 * project/bandTokens for the float bands) and are reached from the renderer —
 * no colour is invented for this lens, in this module or in the view.
 */
import type { ConstructionRow } from '../../../contracts/types';
import type { FloatBand } from '../../../contracts/projectAdapters';
import { OWED_CHIP, type OwedMark } from '../tasks/owedChip.ts';
import type { ActivityNode, PhaseNode, TaskNode } from './activityTree.ts';
import {
  taskDetailStateFor,
  TASK_DETAIL_STATE_LABEL,
  type NoAttemptState,
  type TaskDetailState,
} from '../detail/detailPaneState.ts';

// ---------------------------------------------------------------------------
// The row-state vocabulary
// ---------------------------------------------------------------------------

/**
 * What ONE rendered row is in — the detail pane's `TaskDetailState` extended
 * with the two presentation-only members the LIST needs and a single pane
 * never does.
 *
 * Extending that union (rather than opening a second, parallel one) is
 * deliberate: the pane and the list must never disagree about what a thing is,
 * and `taskDetailStateFill` already resolves every shared member to tokens.
 * That includes `skipped` — a real, terminal, NON-success outcome (see
 * skippedIsNotPassed) — which the pane shows too, from the same
 * detailPaneState.outcomeStateOf mapping. The two additions describe
 * RELATIONSHIPS between rows, which is why they are meaningless to a pane
 * showing exactly one thing:
 *
 *   - `absent`     — named by the data but not carried by this activity's
 *                    profile — e.g. a `CurrentPhase` of `integration` on a
 *                    two-phase `uiDesign` profile, which has no such node.
 *   - `superseded` — an attempt a later attempt replaced.
 */
export type RowState = TaskDetailState | 'absent' | 'superseded';

export const ROW_STATE_LABEL: Record<RowState, string> = {
  ...TASK_DETAIL_STATE_LABEL,
  absent: 'Not in this profile',
  superseded: 'Superseded',
};

/** The subset of RowState that taskDetailStateFill already resolves to tokens. */
export type ChipState = 'running' | 'awaitingHuman' | 'passed' | 'failed';

export interface RowChip {
  /** Rendered uppercase by the view; kept sentence-case here, like the pane's. */
  label: string;
  /** `sm` for the two states that owe a human something; `xs` for the rest. */
  size: 'sm' | 'xs';
  /** Narrowed so the caller can reach taskDetailStateFill without re-narrowing. */
  state: ChipState;
}

/**
 * The ONE chip a row may carry, or nothing.
 *
 * `unknown` returns nothing, and so do `notStarted`, `absent`, `skipped` and
 * `superseded`: each of those has a GEOMETRIC channel of its own (a dashed
 * hairline, a hollow circle, a struck label, reduced opacity, a `↻n` index), and
 * a chip on top of a geometry is the double-encoding the rejected rounds died
 * of. Only the four states that assert something HAPPENED get a chip, and only
 * the two that owe a human something get the larger one.
 */
export function chipFor(state: RowState): RowChip | undefined {
  switch (state) {
    case 'running':
      return { label: ROW_STATE_LABEL.running, size: 'xs', state: 'running' };
    case 'awaitingHuman':
      return { label: ROW_STATE_LABEL.awaitingHuman, size: 'sm', state: 'awaitingHuman' };
    case 'passed':
      return { label: ROW_STATE_LABEL.passed, size: 'xs', state: 'passed' };
    case 'failed':
      return { label: ROW_STATE_LABEL.failed, size: 'sm', state: 'failed' };
    case 'unknown':
    case 'notStarted':
    case 'absent':
    case 'skipped':
    case 'superseded':
      return undefined;
  }
}

// ---------------------------------------------------------------------------
// Magnitudes → geometry (and the numeral that keeps colour from being alone)
// ---------------------------------------------------------------------------

export interface FloatPresentation {
  /** ALWAYS rendered beside the band — WCAG 1.4.1: colour is never the sole
   *  carrier of the criticality. `—` when no float is known. */
  numeral: string;
  /** The server's band, passed through; absent when the caller has none. */
  band?: FloatBand;
  /** False when nothing is known — the view renders a hairline, not a green rail. */
  known: boolean;
}

/**
 * Float as a rail band PLUS a numeral. NetworkNode.tsx already sets this
 * precedent for the same figure on the same project.
 *
 * The band is passed IN, never derived from the number here: the server owns the
 * band policy (0/≤5/≤25/>25 days) and re-deriving it in the view is the
 * hand-mirror defect class this codebase has already paid for twice. An absent
 * float is rendered as absent — a fabricated `0` would read as "on the critical
 * path", the loudest possible lie on this surface.
 */
const FLOAT_BANDS: readonly FloatBand[] = ['critical', 'red', 'yellow', 'green'];

/**
 * The four bands the server's policy emits. `ActivityMeta.band` is a plain
 * string (passed through untouched from the network's compute block), so it is
 * NARROWED here rather than asserted: an unrecognised band is no band at all,
 * the honest reading of a value we cannot place. One narrowing for both lenses.
 */
export function floatBandOf(value: string | undefined): FloatBand | undefined {
  return FLOAT_BANDS.find((b) => b === value);
}

export function floatPresentation(float?: number, band?: FloatBand): FloatPresentation {
  if (float === undefined) return { numeral: '—', known: false };
  return {
    numeral: String(float),
    ...(band !== undefined ? { band } : {}),
    known: true,
  };
}

/**
 * The effort bar's length as a 0..1 fraction of the widest activity on screen,
 * or nothing at all when this activity's effort is unknown.
 *
 * `undefined` rather than `0`: a zero-length bar and "we have no estimate" would
 * be the same pixel, and a row the committed activity list does not carry has
 * no estimate at all. A bar is drawn only where there is a real number behind it.
 */
export function effortBarFraction(
  effortDays: number | undefined,
  maxDays: number
): number | undefined {
  if (effortDays === undefined || maxDays <= 0) return undefined;
  return Math.min(1, Math.max(0, effortDays / maxDays));
}

/** `60%`, or `—` when any profile phase went unreported (App A §1.3 — an
 *  unknown denominator is not a zero numerator). */
export function percentLabel(percentComplete: number | undefined): string {
  return percentComplete === undefined ? '—' : `${String(percentComplete)}%`;
}

/**
 * The `↻N` counter, or nothing.
 *
 * N is the ATTEMPT count, not the retry count, because that is what expands: a
 * task attempted three times stays ONE row showing its latest state, with `↻3`
 * opening the ledger newest-first. A single attempt is the unremarkable case and
 * gets no counter — the counter means "there is history here".
 */
export function retryCounterLabel(attemptCount: number): string | undefined {
  return attemptCount > 1 ? `↻${String(attemptCount)}` : undefined;
}

/** The id column's clamp, in `ch` of the id's own monospace face. */
export const ID_COLUMN_MIN_CH = 12;
export const ID_COLUMN_MAX_CH = 32;

/**
 * The id column's width, in `ch`: sized to the longest id actually on screen,
 * plus one ch of clearance, clamped so one freak id cannot squeeze the title to
 * nothing (32) and a list of short ids still reads as a column (12). A fixed 86px
 * truncated 26 of 29 ids and left two rows reading identically at 1280/1366
 * (designer P0-4). `ch` is resolved on the id cell itself, so it is exact for
 * whichever monospace face the theme sets.
 */
export function idColumnWidthCh(activityIds: readonly string[]): number {
  const longest = activityIds.reduce((max, id) => Math.max(max, id.length), 0);
  return Math.min(ID_COLUMN_MAX_CH, Math.max(ID_COLUMN_MIN_CH, longest + 1));
}

/** 3px on the critical path, 2px otherwise — never a "CRITICAL" chip.
 *  Not-known renders at the neutral 2px: criticality is ASSERTED, never assumed. */
export function criticalBorderPx(onCriticalPath: boolean | undefined): 2 | 3 {
  return onCriticalPath === true ? 3 : 2;
}

// ---------------------------------------------------------------------------
// Row state derivation
// ---------------------------------------------------------------------------

/**
 * The state of a TIER-1 activity row, from the row's own head-state and nothing
 * else — `taskDetailStateFor` with an empty selection is exactly that rule, so
 * the list and the pane cannot drift:
 *
 *   - unclassified            → `unknown`   (no chip: we do not know what it is)
 *   - classified, no evidence → `notStarted`(no chip: we know what it is and
 *                                            have no record of any progress)
 *   - otherwise               → its coarse status, mapped
 */
export function activityRowState(row: ConstructionRow, owed?: OwedMark): RowState {
  // Owed (a live gate, a steer, a recorded failure): the ONE owed vocabulary
  // (tasks/owedChip.ts). Head-state alone never says a human is awaited (Q4).
  if (owed !== undefined) return OWED_CHIP[owed.reason].state;
  return taskDetailStateFor(row, {});
}

/** The chip label an owed activity row carries — "Steer needed" is not "Awaiting
 *  you" — or undefined for the state's own label. */
export function owedChipLabel(owed: OwedMark | undefined): string | undefined {
  return owed !== undefined ? OWED_CHIP[owed.reason].label : undefined;
}

/**
 * The state of a TIER-3 task row, refining the tree's `TaskState` with the two
 * distinctions a ROW can draw and a pure derivation cannot.
 *
 * Both refinements read data the tree already carries; neither contradicts it:
 *
 *  1. `running` on the GATE task the owed set says a human is deciding is
 *     `awaitingHuman` — the live gate (tasks/owedChip.ts), never head-state
 *     `in-review`, and narrowed to that gate task because it is the only task
 *     whose pending outcome the human is actually being asked about.
 *  2. A task with no attempt reads the row's no-attempt state (below).
 *
 * `skipped` needs no refinement: the tree's state already comes from
 * detailPaneState.outcomeStateOf, which keeps it `skipped` (skippedIsNotPassed).
 */
export function taskRowState(
  task: TaskNode,
  /** The activity's owed mark, if any (tasks/owedChip.ts). */
  owed: OwedMark | undefined,
  /** detailPaneState.noAttemptStateFor(the activity's row) — the ONE rule the pane
   *  and its provenance chip read too. Omitted, a task with no attempt stays the
   *  tree's own `unknown`. */
  noAttempt?: NoAttemptState
): RowState {
  if (task.state === 'running' && task.gate && isOwedGate(task, owed)) return 'awaitingHuman';
  // A task with no attempt reads NOT STARTED unless the row is unclassified or its
  // history predates per-task capture (evidence, zero attempts) — designer final
  // items, replacing re-check N3. A row with any attempt has complete history.
  if (task.latestAttempt === undefined && noAttempt !== undefined) return noAttempt;
  return task.state;
}

/** The owed gate is on this task: its gate task where the profile named one, else
 *  its phase. */
function isOwedGate(task: TaskNode, owed: OwedMark | undefined): boolean {
  if (owed?.reason !== 'gate') return false;
  if (owed.gateTask !== undefined) return owed.gateTask === task.task;
  return owed.lifecyclePhase === task.lifecyclePhase;
}

function wordsOf(s: string): string[] {
  return s
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter((w) => w.length > 0);
}

/**
 * The book's task KEY as a task row's trailing secondary text (designer re-check
 * N2), or nothing. It rides only where this profile renamed the task, and never
 * where it would just repeat the label's own word: "Flow Testing" beside
 * `testing`, or "Harness Integration" beside `integration`, says the key twice.
 */
export function bookKeyFor(label: string, bookLabel: string, key: string): string | undefined {
  if (label === bookLabel) return undefined;
  const labelWords = new Set(wordsOf(label));
  return wordsOf(key).every((w) => labelWords.has(w)) ? undefined : key;
}

/**
 * WHY `skipped` is not rendered as `passed` — on the list OR in the pane, which
 * both read detailPaneState.outcomeStateOf (recorded here because the
 * coordinator asked for the reasoning, and because the corpus cannot yet show
 * it — all 214 committed attempts are `passed`).
 *
 * A skipped task did not run. The olive check is the SUCCESS mark on this
 * surface, and lending it to work that never happened is the same false-positive
 * class this whole stage exists to remove — the identical shape as the coarse
 * status that claimed "In construction" for 20 activities with no evidence.
 *
 * The counter-argument, which does not survive: "the phase still exited, so the
 * skip was benign". True, and irrelevant — the phase's exit is the GATE task's
 * verdict, carried by the gate task's own row. A non-gate task that was skipped
 * has no verdict of its own to report, so it reports what it is: skipped,
 * struck, quiet, chip-less. Reading a phase as complete is unaffected: tier 2
 * takes completion from the server, never from summing its tasks.
 */
export const skippedIsNotPassed = true;

/** The state of ONE attempt inside an expanded ledger. */
export function attemptRowState(superseded: boolean, state: RowState): RowState {
  return superseded ? 'superseded' : state;
}

// ---------------------------------------------------------------------------
// The current lifecycle phase, when the profile does not carry it
// ---------------------------------------------------------------------------

export interface CurrentStageMarker {
  /** The canonical phase name the server reported. */
  named: string;
  /** True when a profile node of that name exists — i.e. a row will highlight. */
  inProfile: boolean;
}

/**
 * What the activity's reported current phase resolves to.
 *
 * A reported current phase can name a node the activity's profile does NOT
 * carry (`integration` on a two-phase `uiDesign` profile). Finding no matching
 * node is answered honestly: `inProfile: false` renders the `absent` channel (a gap,
 * 40% opacity, the name struck) beside the row. Silently highlighting the first
 * phase instead — or throwing — would be a fabricated answer to a real question.
 */
export function currentStageMarker(node: ActivityNode): CurrentStageMarker | undefined {
  const named = node.currentLifecyclePhase;
  if (named === undefined) return undefined;
  return { named, inProfile: node.phases.some((p) => (p.phase as string) === named) };
}

/** True when THIS phase node is the activity's reported current one. */
export function isCurrentStage(node: ActivityNode, stage: PhaseNode): boolean {
  return node.currentLifecyclePhase === (stage.phase as string);
}

// ---------------------------------------------------------------------------
// Tier-2 group rule
// ---------------------------------------------------------------------------

/**
 * The `┌ PHASE NAME ···· wt N · exit: …` group RULE, assembled as data.
 *
 * A rule and not a card: three tiers of cards would read as three levels of
 * nesting, and the whole point of this shape is that perceived depth stays at
 * two. The weight is both a numeral (`wt 15`) and a segment width — the segment
 * is the magnitude channel, the numeral is what makes it legible without it.
 */
export interface StageRulePresentation {
  name: string;
  weightLabel: string;
  /** 0..1 of the widest phase in this activity — the segment's width. */
  weightFraction: number;
  exitCriterion: string;
  filled: boolean;
  /** True when the server reported nothing at all about this phase. */
  unreported: boolean;
}

export function stageRule(stage: PhaseNode, maxWeight: number): StageRulePresentation {
  return {
    name: stage.name,
    weightLabel: `wt ${String(stage.weight)}`,
    weightFraction: maxWeight > 0 ? Math.min(1, stage.weight / maxWeight) : 0,
    exitCriterion: stage.exitCriterion,
    filled: stage.completion === 'complete',
    unreported: stage.completion === 'unknown',
  };
}

// ---------------------------------------------------------------------------
// The tier-1 row's columns (designer P1-8, fix-A concern 2)
// ---------------------------------------------------------------------------

/**
 * Below this LIST width (a container query, not the viewport) the kind badge
 * drops to its icon and the float/effort/progress tracks narrow. With the pane
 * open at 1280/1366 the list is ~670-760px; at 1600 it is ~990px.
 */
export const LIST_COMPACT_BELOW_PX = 1000;

/**
 * The title never gets less than this beside the id. Below it the title WRAPS
 * under the id instead of shrinking — and because every row shares the id
 * column's width and this basis, every row wraps or none does. Before this the
 * full-width ids plus the badge cluster squeezed titles to 0px at 1280 with the
 * pane open (fix-A concern 2).
 */
export const TITLE_MIN_PX = 160;

/** The provenance slot fits the spelled-out `≈ RECONSTRUCTED` group badge, which
 *  never abbreviates: it is the tier-1/tier-2 header stamp the spec pins. */
export const PROVENANCE_SLOT_PX = 108;
/** The state slot fits the widest chip ("AWAITING YOU") with its glyph and a `↻N`. */
export const STATE_SLOT_PX = 100;
export const ACTIVITY_GRID_GAP_PX = 6;

export interface ListSlotWidths {
  float: number;
  effort: number;
  kind: number;
  progressTrack: number;
}

/** Fixed slot widths, px. The progress slot is its track plus a 32px numeral and a
 *  4px gap; the provenance and state slots do not vary. */
export const LIST_SLOT_WIDTHS: Readonly<Record<'wide' | 'compact', ListSlotWidths>> = {
  wide: { float: 34, effort: 44, kind: 92, progressTrack: 56 },
  compact: { float: 30, effort: 36, kind: 24, progressTrack: 28 },
};

/** The CSS custom properties one width mode sets; the grid below reads them. */
export function listSlotVars(mode: 'wide' | 'compact'): Record<string, string> {
  const w = LIST_SLOT_WIDTHS[mode];
  return {
    '--list-float-w': `${String(w.float)}px`,
    '--list-effort-w': `${String(w.effort)}px`,
    '--list-kind-w': `${String(w.kind)}px`,
    '--list-progress-track': `${String(w.progressTrack)}px`,
    '--list-progress-w': `${String(w.progressTrack + 36)}px`,
  };
}

/**
 * ONE grid template for every tier-1 row AND the column header, so each column
 * lines up down the list: rail · chevron · float · effort · id+title · kind ·
 * provenance · progress · state. Only the id+title cell flexes.
 */
export function activityGridColumns(railPx: number): string {
  return [
    `${String(railPx)}px`,
    '18px',
    'var(--list-float-w)',
    'var(--list-effort-w)',
    'minmax(0, 1fr)',
    'var(--list-kind-w)',
    `${String(PROVENANCE_SLOT_PX)}px`,
    'var(--list-progress-w)',
    `${String(STATE_SLOT_PX)}px`,
  ].join(' ');
}

/** What the progress slot draws. */
export type ProgressPresentation =
  | { kind: 'fill'; percent: number; label: string }
  | { kind: 'notStarted'; label: string }
  | { kind: 'unknown'; label: string };

/**
 * The progress slot (designer P1-8). A NOT-STARTED activity — classified, nothing
 * recorded — draws an empty dashed track and a muted "0%": the work is known and
 * none of it is done, which is a zero, not an unknown. An unclassified activity
 * (or one with an unreported phase) still reads `—` with no track: there an
 * unknown denominator is not a zero numerator.
 */
export function progressPresentationFor(
  state: RowState,
  percentComplete: number | undefined
): ProgressPresentation {
  if (state === 'notStarted') return { kind: 'notStarted', label: '0%' };
  if (percentComplete === undefined) return { kind: 'unknown', label: percentLabel(undefined) };
  return { kind: 'fill', percent: percentComplete, label: percentLabel(percentComplete) };
}

/** What the state slot draws. */
export type StateSlot =
  | { kind: 'chip'; chip: RowChip }
  | { kind: 'notStarted'; label: string }
  | { kind: 'empty' };

/**
 * The state slot. A chip where `chipFor` gives one; for NOT STARTED the spec's
 * hollow circle plus muted "not started" text — still no chip, so a no-record row
 * reads as not started rather than as missing data (designer P1-8); nothing at all
 * for `unknown`, whose chip-lessness is its signal.
 */
export function stateSlotFor(state: RowState): StateSlot {
  const chip = chipFor(state);
  if (chip !== undefined) return { kind: 'chip', chip };
  if (state === 'notStarted') return { kind: 'notStarted', label: 'not started' };
  return { kind: 'empty' };
}
