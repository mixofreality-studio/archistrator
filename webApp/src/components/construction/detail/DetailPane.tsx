/* eslint-disable react-refresh/only-export-components -- detailActionsFor (+ its two types) is
   re-exported alongside the component per the brief, the same colocation status.tsx/KindBadge.tsx
   already use for their token-driven helpers. */
/**
 * The shared detail pane — ONE header/body/action-bar surface behind every
 * lens (Stage B Task 4). Replaces the old ActivityLifecyclePanel's 480px
 * overlay Drawer (since deleted), which covered the very thing you clicked from
 * and, while open, its modal backdrop also occluded the lens toolbar's right end.
 *
 * >= 1200px: laid out BESIDE the content — default 520px, resizable by
 * dragging the left edge, collapsible to a thin rail. Width is remembered in
 * localStorage (best-effort: every read and write is wrapped in try/catch —
 * it throws outright in a private window or with site data blocked, and the
 * pane must still render correctly with no stored value).
 *
 * 600–1200px: degrades to a Drawer over the right edge (spec §7.4), driven by the
 * shared header/body/action-bar. It is NON-modal, MUI's `persistent` variant, as
 * the graph lens's is (designer re-check #11): no backdrop, no focus trap, and
 * nothing else marked aria-hidden, so the lens toggle and the list stay reachable
 * while it is open. It was a temporary (modal) Drawer, whose backdrop and focus
 * trap put the lens toggle out of reach. Its a11y is kept by hand: focus moves in
 * when it opens, Escape inside it closes it, and on close focus goes back to
 * where it was outside it (the row that opened it, usually).
 *
 * < 600px: the drawer is the whole width, so it covers everything — the toggle
 * included — and a non-modal one let focus walk behind it into content nobody can
 * see (fix-H review I1). There it is MODAL: MUI's `temporary` variant, with its
 * focus trap and aria-modal (ruling: modal at xs, non-modal 600–1200).
 *
 * The two invariants that make this surface trustworthy (see
 * detailPaneState.ts for the pure half of both):
 *
 *   - The HEADER never changes shape across bodies: breadcrumb, state chip,
 *     provenance chip, attempt selector, exit criterion + Table A-1 weight.
 *     A reader must never have to work out which body layout they are
 *     looking at to find out what they have selected.
 *   - The ACTION BAR never changes shape either, and its run action is
 *     present and ENABLED in every state — including `passed` (re-run it)
 *     and `unknown` (run it for the first time). Failure is never terminal,
 *     made structural rather than conditional: see detailActionsFor. Its LABEL
 *     names the selection ("Run this activity / phase / task") and reads ↻
 *     only where an attempt exists, ▶ otherwise (runActionFor, re-check B2).
 *
 * The body slot is filled by ONE of four bodies (Tasks 8–10), chosen by the pure
 * `detailBodyFor` in bodies/bodyDispatch.ts — plus AbsentBody, the by-design
 * sibling of the unknown one.
 *
 * A THIRD invariant joined the header in Task 8: the PROVENANCE chip, and the
 * ProvenanceNote that opens every body. The founder's 2026-09-09 ruling widened
 * a backfill until 21 activities render `100% ✓ PASSED` with every phase
 * complete on evidence that is the ruling itself; the list marks those
 * `≈ RECONSTRUCTED`, and until Task 8 this pane — the surface a reader opens
 * precisely to check such a row — carried no mark at all. See
 * bodies/ProvenanceNote.tsx and detailPaneState.provenanceNodeFor.
 */
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactElement,
  type ReactNode,
} from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Drawer from '@mui/material/Drawer';
import IconButton from '@mui/material/IconButton';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import TextField from '@mui/material/TextField';
import Tooltip from '@mui/material/Tooltip';
import Link from '@mui/material/Link';
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import CheckRoundedIcon from '@mui/icons-material/CheckRounded';
import FirstPageRoundedIcon from '@mui/icons-material/FirstPageRounded';
import LastPageRoundedIcon from '@mui/icons-material/LastPageRounded';
import CloseIcon from '@mui/icons-material/Close';

import type {
  ActivityItem,
  ArtifactModelEnvelope,
  ConstructionReviewSet,
  ConstructionRow,
  ConstructionStage,
  ProjectStateWithGit,
  TaskAttemptRow,
} from '../../../contracts/types';
import { toC4View } from '../../../contracts/adapters';
import {
  activityForComponent,
  contractJoinFor,
  type ContractJoin,
} from '../../../contracts/serviceContracts';
import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { provenanceHatchFill } from '../provenanceAxis.ts';
import {
  useLensSelection,
  type ArtifactViewId,
  type LensSelection,
} from '../lens/useLensSelection';
import { classify } from '../artifactClassification.ts';
import {
  GRADE_LABEL,
  provenanceGradeOf,
  readProvenance,
  type ProvenanceReading,
} from '../provenance';
import {
  attemptsForTask,
  breadcrumbFor,
  decisionActionState,
  detailActionsFor,
  evidencePointerFor,
  headerProvenanceChipsFor,
  liveChipFor,
  observedOnlyChipLabel,
  provenanceNodeFor,
  resolvePhaseTask,
  reviewOnlyActionsFor,
  runActionFor,
  selectedAttemptOf,
  selectionSummaryFor,
  taskDetailStateFill,
  TASK_DETAIL_STATE_LABEL,
  taskDetailStateFor,
  WIDE_PANE_SX,
  type DetailAction,
  type TaskDetailState,
} from './detailPaneState.ts';
import { absenceFor, profileFor } from './bodies/taskBriefing.ts';
import { detailBodyFor, lifecyclePhaseOfTask, selectedTaskIsGate } from './bodies/bodyDispatch.ts';
import {
  artifactRoleFor,
  focusRoleFor,
  focusTargetFor,
  isPrimaryPlacement,
  placementFor,
  type NamedOperation,
  type Placement,
} from './bodies/artifactPlacement.ts';
import {
  ArtifactPlacementView,
  FocusArtifact,
  ReconstructedArtifactNote,
  type PlacementViewContext,
} from './bodies/ArtifactPlacementView';
import { FocusView } from './bodies/FocusView';
import { ScenarioLinkContext, type ScenarioLink } from '../renderers/scenarioLink';
import { AbsentBody } from './bodies/AbsentBody';
import { ArtifactBody, ArtifactStateFrame } from './bodies/ArtifactBody';
import { ProvenanceNote } from './bodies/ProvenanceNote';
import { ReviewBody, ReviewVerdict } from './bodies/ReviewBody';
import { UnknownBody } from './bodies/UnknownBody';
import { hiddenInScope } from '../list/observedOnly';
import { pendingChipLabel, pendingSentence } from '../list/pendingResume.ts';
import {
  paneDecisionApplies,
  sendBackReady,
  type FlowNote,
  type PaneDecision,
  decidedChipLabel,
  decisionLeadFor,
  sendBackCaptionFor,
} from '../tasks/decisionFlow.ts';
import { REVIEW_ONLY_NOTE, owedStateFor, reviewOnlyFor, type OwedMark } from '../tasks/owedChip.ts';

// Re-exported alongside the component per the brief: a caller (and this
// file's own test) can reach the pure invariant without rendering anything.
export { detailActionsFor, type DetailAction, type TaskDetailState } from './detailPaneState.ts';

// ---------------------------------------------------------------------------
// Layout constants
// ---------------------------------------------------------------------------

const MIN_WIDTH = 360;
const MAX_WIDTH = 820;
const DEFAULT_WIDTH = 520;
const WIDTH_STEP = 16;
const COLLAPSED_RAIL_WIDTH = 40;
/** Below this, the beside-content layout has no room left for the content it
 *  sits next to — degrade to the overlay Drawer instead. */
const WIDE_BREAKPOINT = '(min-width:1200px)';
/** Below this the drawer is the whole width, covers everything, and is MODAL (fix-H
 *  review I1). MUI's `sm` breakpoint, where the paper's width goes 100% → 480px. */
const XS_BREAKPOINT = '(max-width:599.95px)';
const WIDTH_STORAGE_KEY = 'archistrator.construction.detailPaneWidth';

/**
 * The contract tab last shown per activity — a module store, so neither the 1.5s
 * poll's remount nor a trip to another activity resets it (designer §3). The
 * URL's `av` wins when present.
 */
const artifactViewStore = new Map<string, ArtifactViewId>();

/**
 * Where focus goes back to when the focus view closes: the control that opened
 * it. The pane's body UNMOUNTS while the focus view is open (polish 3), so the
 * control itself is gone by then — it is remembered as its test id and its index
 * among the pane's controls with that id, and found again once the body is back.
 */
let focusReturnTarget: { testId: string; index: number } | null = null;

function rememberFocusReturn(): void {
  const active = document.activeElement;
  const testId = active instanceof HTMLElement ? active.getAttribute('data-testid') : null;
  if (!(active instanceof HTMLElement) || testId === null) {
    focusReturnTarget = null;
    return;
  }
  const same = Array.from(document.querySelectorAll<HTMLElement>(`[data-testid="${testId}"]`));
  focusReturnTarget = { testId, index: Math.max(0, same.indexOf(active)) };
}

function restoreFocusReturn(): void {
  const target = focusReturnTarget;
  focusReturnTarget = null;
  const candidates = Array.from(
    document.querySelectorAll<HTMLElement>(
      `[data-testid="${target?.testId ?? UI_IDENTIFIERS.Construction.ARTIFACT_FOCUS}"]`
    )
  );
  const found = candidates[target?.index ?? 0] ?? candidates[0];
  (
    found ??
    document.querySelector<HTMLElement>(
      `[data-testid="${UI_IDENTIFIERS.Construction.ARTIFACT_FOCUS}"]`
    )
  )?.focus();
}

/** True when the key event came from a control that takes typing. */
function typingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return (
    target.isContentEditable ||
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    target instanceof HTMLSelectElement ||
    target.getAttribute('role') === 'combobox'
  );
}

function clamp(n: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, n));
}

function readStoredWidth(): number {
  try {
    const stored = window.localStorage.getItem(WIDTH_STORAGE_KEY);
    if (stored !== null) {
      const n = Number(stored);
      if (Number.isFinite(n)) return clamp(n, MIN_WIDTH, MAX_WIDTH);
    }
  } catch {
    // localStorage unavailable (private window, blocked site data) — the
    // pane still renders correctly at the default width.
  }
  return DEFAULT_WIDTH;
}

function writeStoredWidth(width: number): void {
  try {
    window.localStorage.setItem(WIDTH_STORAGE_KEY, String(width));
  } catch {
    // Best-effort persistence only; a dropped write never blocks resizing.
  }
}

// ---------------------------------------------------------------------------
// Public props
// ---------------------------------------------------------------------------

export interface DetailPaneProps {
  selection: LensSelection;
  row: ConstructionRow | undefined;
  /**
   * The committed activity-list slot's activities — the first hop of the
   * contract join (activity → componentId → contractKey → contract), and the
   * inverse hop a Component-tab neighbour click takes. Undefined while unloaded:
   * the join then answers "unresolved", never "no contract".
   */
  activities?: readonly ActivityItem[] | undefined;
  /** Human-readable activity title, falling back to the raw id when absent. */
  activityTitle?: string | undefined;
  /**
   * The EPISODE body's data, supplied by the route.
   *
   * A render prop rather than a fetch: this file is in the pure `components`
   * layer and may not reach into hooks (eslint.platform.config.js). The route
   * hands down `containers/ConstructionEpisodeBodyContainer`, which owns the
   * list/timeline queries and the export assembly. Omitted, the episode body
   * says plainly that episodes are not wired into this mount — never an empty
   * list, which would read as "nothing ever ran".
   */
  episodeSlot?: ((ctx: { activityId: string; attemptId?: string }) => ReactNode) | undefined;
  /** Head-state, for the artifact body's renderers (contracts, testing state). */
  project?: ProjectStateWithGit | undefined;
  /** The committed Phase-1 `system` slot — ServiceContractView's Dynamic tab. */
  systemEnvelope?: ArtifactModelEnvelope | undefined;
  /**
   * The live reviewEngine reviewer set, passed ONLY when the selected activity
   * is the one currently at a phase gate. Passing another activity's set would
   * be the most direct mis-attribution on this surface.
   */
  reviewSet?: ConstructionReviewSet | undefined;
  /**
   * The selected activity's attempts "Observed only" set aside (observedOnly.ts's
   * evidence view), or undefined with the toggle off. `row` is already stripped of
   * them; this is how the pane tells a stripped record from an absent one
   * (designer re-check B1).
   */
  hiddenAttempts?: readonly TaskAttemptRow[] | undefined;
  /**
   * The decision this pane can make (Stage C) — handed down ONLY for an activity
   * the live workflow reports at a gate (tasks/owedWork.ts). Where it applies
   * (paneDecisionApplies: the activity, its gated phase or its gate task) the
   * selection reads AWAITING YOU and Approve / Send back act; Send back asks for
   * the note it will carry. Everywhere else Approve / Send back stay off: a
   * head-state `in-review` row is not a gate, and a button that sends nothing is
   * worse than none (spec §6).
   */
  decision?: PaneDecision | undefined;
  /**
   * The selected activity's owed mark (tasks/owedChip.ts) and, for a steer or a
   * failure, the reason as a sentence. The ONE source of the pane's AWAITING YOU,
   * STEER NEEDED and FAILED chips (review I4, designer P0-2): head-state
   * `in-review` no longer says a human is awaited. A steer-needed or failed
   * activity is REVIEW-ONLY until follow-up B1 delivers the operator's note to the
   * agent (the PM's must-hold): no actions at all, and a muted line saying so.
   */
  owed?: { mark: OwedMark; sentence?: string | undefined } | undefined;
  /** The next owed decision, offered in the drawer's footer below 1200px, where
   *  the TASKS table is hidden behind the drawer (designer P2). */
  nextDecision?: { label: string; onClick: () => void } | undefined;
  /** The selected activity's live session stage (`null`: none exists; undefined:
   *  not probed). Where the ledger cannot place the selection, the chip says what
   *  the workflow says of the activity instead of UNKNOWN (liveChipFor). */
  liveStage?: ConstructionStage | null | undefined;
  onClose: () => void;
}

export function DetailPane({
  selection,
  row,
  activities,
  activityTitle,
  episodeSlot,
  project,
  systemEnvelope,
  reviewSet,
  hiddenAttempts,
  decision,
  owed,
  nextDecision,
  liveStage,
  onClose,
}: DetailPaneProps): ReactElement | null {
  const t = useTokens();
  const { select, artifact, setArtifactView, setScenario, setFocus } = useLensSelection();
  const isWide = useMediaQuery(WIDE_BREAKPOINT);
  const modal = useMediaQuery(XS_BREAKPOINT);

  const [width, setWidth] = useState<number>(readStoredWidth);
  const [collapsed, setCollapsed] = useState(false);
  const [resizing, setResizing] = useState(false);
  const widthRef = useRef(width);
  useEffect(() => {
    widthRef.current = width;
  }, [width]);

  const activityId = selection.activityId;
  const open = activityId !== undefined;

  const onResizePointerDown = useCallback((e: ReactPointerEvent<HTMLDivElement>): void => {
    e.preventDefault();
    setResizing(true);
  }, []);

  useEffect(() => {
    if (!resizing) return undefined;
    const onMove = (e: PointerEvent): void => {
      // The pane is anchored to the viewport's right edge, so its width is the
      // distance from the pointer to that edge.
      setWidth(clamp(window.innerWidth - e.clientX, MIN_WIDTH, MAX_WIDTH));
    };
    const onUp = (): void => {
      setResizing(false);
      writeStoredWidth(widthRef.current);
    };
    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    return (): void => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
    };
  }, [resizing]);

  const onResizeKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>): void => {
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      setWidth((w) => {
        const next = clamp(w + WIDTH_STEP, MIN_WIDTH, MAX_WIDTH);
        writeStoredWidth(next);
        return next;
      });
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      setWidth((w) => {
        const next = clamp(w - WIDTH_STEP, MIN_WIDTH, MAX_WIDTH);
        writeStoredWidth(next);
        return next;
      });
    }
  }, []);

  // Below 1200px the pane is a NON-modal drawer (see its render below), so the
  // focus handling a modal would do is done here: focus moves into the drawer when
  // it opens, and when it closes focus goes back to whatever OUTSIDE it last had
  // focus (the row that opened it, or the one the operator moved to since).
  const narrowOpen = open && !isWide;
  const drawerBodyRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!narrowOpen) return undefined;
    const inDrawer = (node: EventTarget | null): boolean =>
      node instanceof Node && drawerBodyRef.current?.contains(node) === true;
    const active = document.activeElement;
    let returnTo: HTMLElement | null =
      active instanceof HTMLElement && active !== document.body ? active : null;
    const onFocusIn = (e: FocusEvent): void => {
      if (e.target instanceof HTMLElement && !inDrawer(e.target)) returnTo = e.target;
    };
    document.addEventListener('focusin', onFocusIn);
    drawerBodyRef.current?.focus();
    return (): void => {
      document.removeEventListener('focusin', onFocusIn);
      if (returnTo?.isConnected === true) returnTo.focus();
    };
  }, [narrowOpen]);

  // Stage C: a live gate handed down by the route makes the gated selection
  // AWAITING YOU — the one source of that state; head-state `in-review` is not it.
  const decisionApplies = decision !== undefined && paneDecisionApplies(decision, selection);
  const decisionLive = decisionApplies && decision.open;
  // The owed set says what is owed here, and nothing else does (review I4).
  const owedChip = owedStateFor(owed?.mark, selection);
  const reviewOnly = reviewOnlyFor(owed?.mark, selection);
  const state = useMemo(
    (): TaskDetailState => owedChip?.state ?? taskDetailStateFor(row, selection),
    [owedChip, row, selection]
  );
  // The send-back composer, open for one activity at a time — keyed by the
  // activity rather than reset in an effect, so selecting elsewhere closes it.
  const [composingFor, setComposingFor] = useState<string | null>(null);
  const [sendBackNote, setSendBackNote] = useState('');
  const composing = decisionLive && composingFor === activityId;
  // Approve / Send back act only where a live decision backs them (and not while
  // one is in flight). Run is present in every state, disabled with its reason
  // until the console can start the work (spec §7.8, §9.3).
  // A steer-needed or failed activity is review-only (PM must-hold): Run alone,
  // disabled with the review-only reason (tasks merge review I2 ruling).
  // While the composer is open, its own "Send back with this note" is the send:
  // the bar's Send back steps aside (tasks round 2, designer).
  const actions = useMemo(
    () =>
      reviewOnly
        ? reviewOnlyActionsFor(runActionFor(row, selection))
        : detailActionsFor(state, runActionFor(row, selection))
            .filter((a) => !(composing && a.id === 'sendBack'))
            .map(
              (a): DetailAction =>
                a.id === 'run' ? a : { ...a, ...decisionActionState(decision, decisionApplies) }
            ),
    [reviewOnly, state, row, selection, decision, decisionApplies, composing]
  );
  // After a decision, while its record lives, the chip says what was decided
  // (designer P1-4): "Decided · approved" / "Sent back". Where nothing is owed or
  // decided and the ledger cannot place the selection, the activity's live state.
  const decided = decisionApplies ? decision.decided : undefined;
  const live =
    decided === undefined && owedChip === undefined ? liveChipFor(state, liveStage) : undefined;
  // An integration-pending activity names its phase: "Integration pending".
  const pendingLabel = state === 'waiting' && row !== undefined ? pendingChipLabel(row) : undefined;
  const stateLabel =
    decided !== undefined
      ? decidedChipLabel(decided.decision)
      : (owedChip?.label ?? live?.label ?? pendingLabel);
  // ...and says what it waits on, whenever no single task is selected: the task's
  // own attempt is what a task selection is about.
  const pendingLine =
    selection.task === undefined && row !== undefined ? pendingSentence(row) : undefined;
  const chipState: TaskDetailState =
    decided !== undefined
      ? decided.decision === 'approve'
        ? 'passed'
        : 'running'
      : (live?.state ?? state);
  const owedReason = reviewOnly ? owed?.sentence : undefined;
  // Attempts "Observed only" set aside in whatever is selected — above zero, the
  // chip and the unknown card say so rather than calling the record absent (B1).
  const hiddenCount = useMemo(
    () => hiddenInScope(hiddenAttempts, selection),
    [hiddenAttempts, selection]
  );
  const meta = useMemo(() => resolvePhaseTask(row, selection), [row, selection]);
  // "N attempts · M phases" (or "· M tasks" for a phase) when no single task is
  // selected — in place of an attempt selector that has nothing to select.
  const summary = useMemo(() => selectionSummaryFor(row, selection), [row, selection]);
  // Provenance read through the SAME axis the list's rail and badge read
  // (provenanceAxis.ts), scoped to whatever is selected — see provenanceNodeFor
  // for why the pane carrying no mark was laundering the founder's ruling.
  const provenance = useMemo(
    () => readProvenance(provenanceNodeFor(row, selection)),
    [row, selection]
  );
  const evidence = useMemo(() => evidencePointerFor(row, selection), [row, selection]);
  // The attempt KEY (`<activityId>:<task>:<n>`) — the only string an episode's
  // TargetRef may be compared against to claim it for this task.
  const selectedAttempt = useMemo(() => selectedAttemptOf(row, selection), [row, selection]);
  const taskAttempts = useMemo(
    () =>
      selection.task !== undefined ? attemptsForTask(row?.attempts ?? [], selection.task) : [],
    [row, selection.task]
  );
  const effectiveAttempt = selection.attempt ?? taskAttempts[taskAttempts.length - 1]?.attempt;

  const label = activityTitle ?? activityId ?? '—';
  const breadcrumb = breadcrumbFor(label, meta.phaseName, meta.taskLabel, effectiveAttempt);

  // --- The committed artifact at this selection (renderers-placement §2) ------
  const c4 = useMemo(() => toC4View(systemEnvelope), [systemEnvelope]);
  const joinInput = useMemo(
    () => ({ activities, components: c4.components, contracts: project?.serviceContracts }),
    [activities, c4.components, project?.serviceContracts]
  );
  const join = useMemo(
    (): ContractJoin | undefined =>
      activityId !== undefined ? contractJoinFor(joinInput, activityId) : undefined,
    [joinInput, activityId]
  );
  const placement = useMemo(() => placementFor(row, selection, join), [row, selection, join]);
  const primary = isPrimaryPlacement(placement);
  // UNDER REVIEW: a gate task, owed NOW (the live workflow at it), on an
  // observed attempt — never a reconstructed or stripped one (§1, §4.3).
  const gateOwedNow = decisionLive || (owed?.mark.reason === 'gate' && owedChip !== undefined);
  const role = artifactRoleFor({
    gateSelected: selectedTaskIsGate(row, selection),
    gateOwedNow,
    attemptOrigin: selectedAttempt?.provenance.origin,
    hiddenCount,
  });
  const reconstructed = provenance.origin === 'backfilled' || provenance.origin === 'synthesized';
  const stpLatest = attemptsForTask(row?.attempts ?? [], 'stp').at(-1);
  const focusTarget = focusTargetFor(placement, join);
  const focusOpen = open && artifact?.focus === true && focusTarget !== undefined;
  const view: ArtifactViewId =
    artifact?.view ??
    (activityId !== undefined ? artifactViewStore.get(activityId) : undefined) ??
    'code';

  const enterFocus = useCallback((): void => {
    rememberFocusReturn();
    setFocus(true);
  }, [setFocus]);
  const exitFocus = useCallback((): void => {
    setFocus(false);
  }, [setFocus]);
  // Back to the control that opened it — or, after a deep link, the pane's own
  // Focus button — once the layer is gone AND the pane's body is back (it was
  // unmounted while the focus view was open), i.e. after the render that closed it.
  const wasFocusOpen = useRef(focusOpen);
  useEffect(() => {
    if (wasFocusOpen.current && !focusOpen) restoreFocusReturn();
    wasFocusOpen.current = focusOpen;
  }, [focusOpen]);

  // The system test plan's scenario deep link (`sc`), read by its browser (B2).
  const scenarioLink = useMemo(
    (): ScenarioLink => ({ scenarioId: artifact?.scenario, onScenarioChange: setScenario }),
    [artifact?.scenario, setScenario]
  );
  const planRowId = useMemo(
    () =>
      Object.values(project?.constructionRows ?? {}).find((r) => classify(r) === 'testing:plan')
        ?.activityId,
    [project?.constructionRows]
  );

  const placementCtx = useMemo((): PlacementViewContext => {
    const componentId = join !== undefined && 'componentId' in join ? join.componentId : undefined;
    const inboundOperations: NamedOperation[] =
      componentId !== undefined
        ? c4.relationships
            .filter((r) => r.to === componentId)
            .map((r) => ({ label: r.label, calledBy: r.from }))
        : [];
    const othersMissing = (activities ?? []).filter(
      (a) => a.name !== activityId && contractJoinFor(joinInput, a.name).kind === 'missing'
    ).length;
    const stpOrigin = stpLatest?.provenance.origin;
    return {
      join,
      activityKind: row?.kind,
      role,
      observedOnly: hiddenAttempts !== undefined,
      reconstructedScope: reconstructed
        ? selection.task !== undefined
          ? 'task'
          : 'wider'
        : undefined,
      project,
      systemEnvelope,
      compact: modal,
      inFocus: false,
      view,
      onViewChange: (next): void => {
        if (activityId !== undefined) artifactViewStore.set(activityId, next);
        setArtifactView(next);
      },
      onFocusComponent: (componentId): void => {
        // A neighbour's activity, at the same phase and task where its own
        // profile has them, on the same tab. A neighbour no activity builds (a
        // utility) has nowhere to go, so the click does nothing.
        const next = activityForComponent(activities, componentId);
        if (next === undefined) return;
        const nextRow = project?.constructionRows?.[next];
        const task =
          selection.task !== undefined &&
          lifecyclePhaseOfTask(nextRow, selection.task) !== undefined
            ? selection.task
            : undefined;
        const phase =
          selection.lifecyclePhase !== undefined &&
          (profileFor(nextRow) ?? []).some((p) => p.phase === selection.lifecyclePhase)
            ? selection.lifecyclePhase
            : undefined;
        artifactViewStore.set(next, 'component');
        select(
          {
            activityId: next,
            ...(phase !== undefined ? { lifecyclePhase: phase } : {}),
            ...(task !== undefined ? { task } : {}),
          },
          { view: 'component', ...(artifact?.focus === true ? { focus: true as const } : {}) }
        );
      },
      onOpenDesign: (): void => {
        if (activityId !== undefined) select({ activityId, lifecyclePhase: 'detailed_design' });
      },
      onOpenDynamic: (): void => {
        if (activityId === undefined) return;
        artifactViewStore.set(activityId, 'dynamic');
        select({ activityId, lifecyclePhase: 'detailed_design' }, { view: 'dynamic' });
      },
      onFocus: focusTarget !== undefined ? enterFocus : undefined,
      // THIS row's scenario, deep-linked (`sc`), never the plan's first (B2).
      onOpenSystemTestPlan:
        planRowId !== undefined
          ? (scenarioId): void => {
              select({ activityId: planRowId }, { scenario: scenarioId });
            }
          : undefined,
      systemTestPlanId: planRowId,
      isNavigable: (componentId): boolean =>
        activityForComponent(activities, componentId) !== undefined,
      evidence,
      attemptOrigin: selectedAttempt?.provenance.origin,
      attemptNumber: selectedAttempt?.attempt,
      gateOwedNow,
      produced: row?.produced ?? [],
      stpReconstructed: stpOrigin === 'backfilled' || stpOrigin === 'synthesized',
      othersMissing,
      inboundOperations,
    };
  }, [
    join,
    c4.relationships,
    activities,
    activityId,
    joinInput,
    stpLatest,
    project,
    row?.kind,
    role,
    hiddenAttempts,
    reconstructed,
    selection,
    systemEnvelope,
    modal,
    view,
    setArtifactView,
    select,
    artifact?.focus,
    focusTarget,
    enterFocus,
    evidence,
    planRowId,
    selectedAttempt,
    gateOwedNow,
    row?.produced,
  ]);

  if (!open) return null;

  // `F` while the pane has focus opens the focus view (§3) — never while typing.
  const onBodyKeyDown = (e: React.KeyboardEvent<HTMLElement>): void => {
    if (e.key !== 'f' && e.key !== 'F') return;
    if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
    if (focusTarget === undefined || typingTarget(e.target)) return;
    e.preventDefault();
    enterFocus();
  };

  const onSelectAttempt = (attempt: number): void => {
    select({ ...selection, attempt });
  };

  const onAction = (id: DetailAction['id']): void => {
    if (decision === undefined || !decisionLive) return;
    if (id === 'approve') decision.onApprove();
    else if (id === 'sendBack') setComposingFor(activityId);
  };
  const actionBar = (
    <ActionBar
      actions={actions}
      composer={
        composing ? (
          <SendBackComposer
            anchoredCount={decision.anchoredCount}
            caption={sendBackCaptionFor(decision.lifecyclePhase)}
            note={sendBackNote}
            t={t}
            onCancel={() => {
              setComposingFor(null);
            }}
            onChange={setSendBackNote}
            onSend={() => {
              decision.onSendBack(sendBackNote);
              setComposingFor(null);
              setSendBackNote('');
            }}
          />
        ) : undefined
      }
      flow={decisionApplies ? decision.note : undefined}
      reviewOnlyNote={reviewOnly ? REVIEW_ONLY_NOTE : undefined}
      t={t}
      onAction={onAction}
    />
  );

  const bodyKind = detailBodyFor(row, selection, state, project, primary);
  // While the focus view is open the pane's body is UNMOUNTED (polish 3): the
  // artifact lives in the focus view, and a second copy under it duplicated every
  // test id and DOM id. One line says where it went.
  const body = focusOpen ? (
    <Typography
      data-testid={UI_IDENTIFIERS.Construction.FOCUS_PLACEHOLDER}
      sx={{ fontFamily: t.body, fontSize: 12.5, color: t.muted }}
    >
      Showing in focus view.
    </Typography>
  ) : (
    <ScenarioLinkContext.Provider value={scenarioLink}>
      {/* Invariant across every body — provenance is an ORTHOGONAL axis, so
          which body is showing must never change whether the reader is told how
          the record came to exist. Above a committed artifact it is condensed so
          the artifact stays above the fold (B1); never dropped. */}
      {decided !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_DECISION_LEAD}
          sx={{ fontFamily: t.body, fontSize: 12.5, fontWeight: 600, color: t.ink, mb: 1.25 }}
        >
          {decisionLeadFor(decided)}
        </Typography>
      ) : null}
      <ProvenanceNote condensed={primary} evidence={evidence} reading={provenance} />
      <Box sx={{ minWidth: 0 }} onKeyDown={onBodyKeyDown}>
        <DetailBody
          activityTitle={activityTitle}
          episodeSlot={episodeSlot}
          hiddenCount={hiddenCount}
          placement={placement}
          placementCtx={placementCtx}
          primary={primary}
          project={project}
          reviewSet={reviewSet}
          row={row}
          selectedAttemptId={selectedAttempt?.attemptId}
          selection={selection}
          state={state}
          systemEnvelope={systemEnvelope}
        />
      </Box>
    </ScenarioLinkContext.Provider>
  );

  // The focus view (§3): the same header and action bar, the artifact at full
  // width, the lens still mounted underneath. `focus=1` with nothing to focus is
  // ignored rather than opening an empty layer.
  const focusLayer = focusOpen ? (
    <FocusView
      open
      actionBar={actionBar}
      header={
        <DetailHeader
          breadcrumb={breadcrumb}
          chipState={chipState}
          exitCriterion={meta.exitCriterion}
          hiddenCount={hiddenCount}
          owedReason={owedReason}
          pendingLine={pendingLine}
          provenance={provenance}
          state={state}
          stateLabel={stateLabel}
          summary={summary}
          t={t}
          taskAttempts={taskAttempts}
          taskSelected={selection.task !== undefined}
          titleId="construction-focus-pane-title"
          weight={meta.phaseWeight}
          onClose={exitFocus}
          onSelectAttempt={onSelectAttempt}
        />
      }
      rail={
        // What judges the artifact sits in the rail beside it (polish 1): the
        // attempt's full provenance note, the "nothing links it" sentence, and a
        // review's verdict — which must appear in focus.
        <>
          <ProvenanceNote evidence={evidence} reading={provenance} />
          <ReconstructedArtifactNote ctx={placementCtx} />
          {bodyKind === 'review' ? <ReviewVerdict reviewSet={reviewSet} row={row} /> : null}
        </>
      }
      onClose={exitFocus}
    >
      <ScenarioLinkContext.Provider value={scenarioLink}>
        <FocusArtifact
          artifactRole={focusRoleFor(placement, role)}
          ctx={placementCtx}
          target={focusTarget}
        />
      </ScenarioLinkContext.Provider>
    </FocusView>
  ) : null;

  const paneContent = (
    <DetailPaneChrome
      actionBar={actionBar}
      body={body}
      breadcrumb={breadcrumb}
      chipState={chipState}
      collapsed={collapsed}
      exitCriterion={meta.exitCriterion}
      hiddenCount={hiddenCount}
      owedReason={owedReason}
      pendingLine={pendingLine}
      provenance={provenance}
      state={state}
      stateLabel={stateLabel}
      summary={summary}
      t={t}
      taskAttempts={taskAttempts}
      taskSelected={selection.task !== undefined}
      weight={meta.phaseWeight}
      width={width}
      onClose={onClose}
      onResizeKeyDown={onResizeKeyDown}
      onResizePointerDown={onResizePointerDown}
      onSelectAttempt={onSelectAttempt}
      onToggleCollapsed={() => {
        setCollapsed((c) => !c);
      }}
    />
  );

  if (isWide) {
    return (
      <Box data-testid={UI_IDENTIFIERS.Construction.DETAIL_PANE} sx={WIDE_PANE_SX}>
        {paneContent}
        {focusLayer}
      </Box>
    );
  }

  // Below 1200px the pane cannot sit beside content that no longer has room for it,
  // so it is a Drawer over the right edge. From 600px it is a PERSISTENT one, which
  // renders no Modal at all. A temporary Drawer is a Modal even with no backdrop and
  // no focus trap: MUI's ModalManager marks every sibling of its container
  // aria-hidden while it is open (found on the graph branch). The persistent variant
  // ignores onClose, so Escape is handled on the body below.
  //
  // Below 600px it covers the whole screen, so it IS modal: the temporary variant,
  // whose focus trap keeps Tab inside it and whose aria-modal says so (fix-H review
  // I1). Escape still closes it through the body's own handler.
  return (
    <Drawer
      anchor="right"
      data-modal={String(modal)}
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_DRAWER}
      open={open}
      slotProps={{
        paper: {
          'aria-labelledby': 'construction-detail-pane-title',
          'aria-modal': modal,
          role: 'dialog',
          sx: { width: { xs: '100%', sm: 480 }, bgcolor: t.paper, backgroundImage: 'none' },
        },
      }}
      variant={modal ? 'temporary' : 'persistent'}
      onClose={modal ? onClose : undefined}
    >
      <Box
        ref={drawerBodyRef}
        sx={{
          display: 'flex',
          flexDirection: 'column',
          height: '100%',
          // Focus lands here programmatically (it is the dialog's content, not a
          // control): no ring round the whole pane; the first Tab reaches its controls.
          outline: 0,
          '&:focus-visible': { boxShadow: 'none' },
        }}
        tabIndex={-1}
        onKeyDown={(e) => {
          // Escape inside the drawer closes it. Only a key pressed in the drawer's
          // own DOM: React bubbles portal events (a Select's open menu) through here
          // too, and that Escape belongs to the menu.
          if (e.key !== 'Escape' || e.defaultPrevented) return;
          if (!(e.target instanceof Node) || !e.currentTarget.contains(e.target)) return;
          e.stopPropagation();
          onClose();
        }}
      >
        <DetailHeader
          breadcrumb={breadcrumb}
          chipState={chipState}
          exitCriterion={meta.exitCriterion}
          hiddenCount={hiddenCount}
          owedReason={owedReason}
          pendingLine={pendingLine}
          provenance={provenance}
          state={state}
          stateLabel={stateLabel}
          summary={summary}
          t={t}
          taskAttempts={taskAttempts}
          taskSelected={selection.task !== undefined}
          weight={meta.phaseWeight}
          onClose={onClose}
          onSelectAttempt={onSelectAttempt}
        />
        <Box
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY}
          sx={{ flexGrow: 1, overflowY: 'auto', px: 2.5, py: 2 }}
        >
          {body}
        </Box>
        {actionBar}
        {nextDecision !== undefined ? (
          <Box sx={{ px: 2, py: 1, borderTop: `1px solid ${t.line}`, bgcolor: t.paperAlt }}>
            <Link
              component="button"
              data-testid={UI_IDENTIFIERS.Construction.DETAIL_NEXT_DECISION}
              sx={{ fontFamily: t.mono, fontSize: 11.5, fontWeight: 700, color: t.accent2 }}
              underline="hover"
              onClick={nextDecision.onClick}
            >
              {nextDecision.label}
            </Link>
          </Box>
        ) : null}
      </Box>
      {focusLayer}
    </Drawer>
  );
}

// ---------------------------------------------------------------------------
// The beside-content chrome — collapsible rail + resize handle + the same
// header/body/action-bar the Drawer path renders.
// ---------------------------------------------------------------------------

function DetailPaneChrome({
  width,
  collapsed,
  t,
  breadcrumb,
  state,
  provenance,
  summary,
  taskAttempts,
  exitCriterion,
  weight,
  body,
  actionBar,
  hiddenCount,
  taskSelected,
  stateLabel,
  chipState,
  owedReason,
  pendingLine,
  onClose,
  onToggleCollapsed,
  onResizePointerDown,
  onResizeKeyDown,
  onSelectAttempt,
}: {
  width: number;
  collapsed: boolean;
  t: Tokens;
  breadcrumb: string;
  state: TaskDetailState;
  stateLabel: string | undefined;
  chipState: TaskDetailState;
  owedReason: string | undefined;
  pendingLine: string | undefined;
  provenance: ProvenanceReading;
  hiddenCount: number;
  taskSelected: boolean;
  summary: string | undefined;
  taskAttempts: ReturnType<typeof attemptsForTask>;
  exitCriterion: string | undefined;
  weight: number | undefined;
  body: ReactElement;
  /** The invariant action bar, built once in DetailPane so this path and the
   *  Drawer path render the same one. */
  actionBar: ReactElement;
  onClose: () => void;
  onToggleCollapsed: () => void;
  onResizePointerDown: (e: ReactPointerEvent<HTMLDivElement>) => void;
  onResizeKeyDown: (e: React.KeyboardEvent<HTMLDivElement>) => void;
  onSelectAttempt: (attempt: number) => void;
}): ReactElement {
  if (collapsed) {
    return (
      <Box
        sx={{
          flexShrink: 0,
          width: COLLAPSED_RAIL_WIDTH,
          borderLeft: `1.5px solid ${t.line}`,
          bgcolor: t.paperAlt,
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          pt: 1,
        }}
      >
        <Tooltip placement="left" title="Expand the detail pane">
          <IconButton
            aria-label="expand detail pane"
            data-testid={UI_IDENTIFIERS.Construction.DETAIL_COLLAPSE_TOGGLE}
            size="small"
            sx={{ color: t.ink }}
            onClick={onToggleCollapsed}
          >
            {/* |<- : the pane comes back out from the edge it folded into. */}
            <FirstPageRoundedIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Box>
    );
  }

  return (
    <>
      <Box
        aria-label="Resize detail pane"
        aria-orientation="vertical"
        aria-valuemax={MAX_WIDTH}
        aria-valuemin={MIN_WIDTH}
        aria-valuenow={width}
        data-testid={UI_IDENTIFIERS.Construction.DETAIL_RESIZE_HANDLE}
        role="separator"
        sx={{
          flexShrink: 0,
          width: 6,
          cursor: 'col-resize',
          bgcolor: 'transparent',
          '&:hover': { bgcolor: t.line },
          '&:focus-visible': { bgcolor: t.accent, outline: 'none' },
        }}
        tabIndex={0}
        onKeyDown={onResizeKeyDown}
        onPointerDown={onResizePointerDown}
      />
      <Box
        sx={{
          flexShrink: 0,
          width,
          display: 'flex',
          flexDirection: 'column',
          borderLeft: `1.5px solid ${t.line}`,
          bgcolor: t.paper,
          minHeight: 0,
        }}
      >
        <DetailHeader
          breadcrumb={breadcrumb}
          chipState={chipState}
          exitCriterion={exitCriterion}
          hiddenCount={hiddenCount}
          owedReason={owedReason}
          pendingLine={pendingLine}
          provenance={provenance}
          state={state}
          stateLabel={stateLabel}
          summary={summary}
          t={t}
          taskAttempts={taskAttempts}
          taskSelected={taskSelected}
          weight={weight}
          onClose={onClose}
          onCollapse={onToggleCollapsed}
          onSelectAttempt={onSelectAttempt}
        />
        <Box
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_BODY}
          sx={{ flexGrow: 1, overflowY: 'auto', px: 2.5, py: 2, minHeight: 0 }}
        >
          {body}
        </Box>
        {actionBar}
      </Box>
    </>
  );
}

// ---------------------------------------------------------------------------
// Header — invariant across every body: breadcrumb, state chip, provenance
// chip, attempt selector, exit criterion + Table A-1 weight.
// ---------------------------------------------------------------------------

function DetailHeader({
  breadcrumb,
  state,
  provenance,
  summary,
  taskAttempts,
  exitCriterion,
  weight,
  t,
  onClose,
  onCollapse,
  onSelectAttempt,
  hiddenCount,
  taskSelected,
  stateLabel,
  chipState,
  owedReason,
  pendingLine,
  titleId = 'construction-detail-pane-title',
}: {
  /** The breadcrumb's element id; the focus view's copy of the header takes its own. */
  titleId?: string;
  breadcrumb: string;
  state: TaskDetailState;
  /** The owed chip's own word, when the owed set says what is owed here. */
  stateLabel: string | undefined;
  /** The chip's fill — the state, unless a decision just made says otherwise. */
  chipState: TaskDetailState;
  /** A steer's or a failure's reason, as a sentence (designer P0-2). */
  owedReason: string | undefined;
  /** An integration-pending activity's sentence (pendingResume.pendingSentence). */
  pendingLine: string | undefined;
  provenance: ProvenanceReading;
  /** Attempts "Observed only" hid in the selection (B1); 0 with the toggle off. */
  hiddenCount: number;
  /** A single task is selected (its state decides whether a grade exists to show). */
  taskSelected: boolean;
  /** "N attempts · M phases" when no single task is selected (selectionSummaryFor). */
  summary: string | undefined;
  taskAttempts: ReturnType<typeof attemptsForTask>;
  exitCriterion: string | undefined;
  weight: number | undefined;
  t: Tokens;
  onClose: () => void;
  onCollapse?: () => void;
  onSelectAttempt: (attempt: number) => void;
}): ReactElement {
  const fill = taskDetailStateFill(t, chipState);

  return (
    <Box
      sx={{
        flexShrink: 0,
        px: 2,
        py: 1.5,
        borderBottom: `1.5px solid ${t.line}`,
        borderTop: `4px solid ${t.accent}`,
        bgcolor: t.paperAlt,
      }}
    >
      <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 0.5 }}>
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_BREADCRUMB}
          id={titleId}
          sx={{
            flexGrow: 1,
            minWidth: 0,
            fontFamily: t.mono,
            fontSize: 12.5,
            fontWeight: 700,
            color: t.ink,
            lineHeight: 1.3,
            wordBreak: 'break-word',
          }}
        >
          {breadcrumb}
        </Typography>
        {onCollapse !== undefined && (
          <Tooltip placement="bottom" title="Collapse the detail pane — your selection stays">
            <IconButton
              aria-label="collapse detail pane"
              data-testid={UI_IDENTIFIERS.Construction.DETAIL_COLLAPSE_TOGGLE}
              size="small"
              sx={{ color: t.muted }}
              onClick={onCollapse}
            >
              {/* ->| : a collapse-PANE mark, not a next-page chevron (adopted P2). */}
              <LastPageRoundedIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        )}
        <IconButton
          aria-label="close detail pane"
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_CLOSE}
          size="small"
          sx={{ color: t.ink }}
          onClick={onClose}
        >
          <CloseIcon fontSize="small" />
        </IconButton>
      </Box>

      <Box sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 0.75, mt: 1 }}>
        <Box
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_STATE_CHIP}
          sx={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 0.5,
            px: 0.75,
            py: 0.2,
            borderRadius: 99,
            border: `1px solid ${fill.border}`,
            bgcolor: fill.bg,
            color: fill.fg,
            fontFamily: t.mono,
            fontSize: 9.5,
            fontWeight: 700,
            letterSpacing: '0.06em',
            whiteSpace: 'nowrap',
          }}
        >
          {(stateLabel ?? TASK_DETAIL_STATE_LABEL[state]).toUpperCase()}
        </Box>

        <ProvenanceChips
          hiddenCount={hiddenCount}
          provenance={provenance}
          state={state}
          t={t}
          taskSelected={taskSelected}
        />

        {/* An activity or phase selection has no single task to pick an attempt
            of, so it says what it holds ("N attempts · M phases") instead of an
            empty selector reading "NO ATTEMPTS" beside a PASSED chip (P1-6). */}
        {summary !== undefined ? (
          <Typography
            data-testid={UI_IDENTIFIERS.Construction.DETAIL_SELECTION_SUMMARY}
            sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, letterSpacing: '0.04em' }}
          >
            {summary}
          </Typography>
        ) : (
          <AttemptSelector attempts={taskAttempts} t={t} onSelectAttempt={onSelectAttempt} />
        )}
      </Box>

      {owedReason !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_OWED_REASON}
          sx={{
            fontFamily: t.body,
            fontSize: 12.5,
            fontWeight: 600,
            color: state === 'failed' ? t.dangerFg : t.awaitingFg,
            lineHeight: 1.4,
            mt: 1,
          }}
        >
          {owedReason}
        </Typography>
      ) : null}

      {/* "Integration pending — waits on C-a (not built), …": what the WAITING
          chip is waiting on. Ink, not the awaiting tone — nothing is owed here. */}
      {pendingLine !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_PENDING_RESUME}
          sx={{
            fontFamily: t.body,
            fontSize: 12.5,
            fontWeight: 600,
            color: t.ink,
            lineHeight: 1.4,
            mt: 1,
          }}
        >
          {pendingLine}
        </Typography>
      ) : null}

      {/* Only when a phase applies: an "Exit: —" line with nothing after it said
          less than no line at all (P1-6). The weight comes from the same phase. */}
      {exitCriterion !== undefined ? (
        <Box
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_EXIT_CRITERION}
          sx={{ display: 'flex', alignItems: 'baseline', gap: 0.75, mt: 1 }}
        >
          <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.4 }}>
            Exit: {exitCriterion}
          </Typography>
          {weight !== undefined && (
            <Typography
              sx={{
                fontFamily: t.mono,
                fontSize: 10.5,
                fontWeight: 700,
                color: t.muted,
                flexShrink: 0,
              }}
            >
              · {String(weight)}%
            </Typography>
          )}
        </Box>
      ) : null}
    </Box>
  );
}

/**
 * The header's provenance chip — the mark the pane was missing.
 *
 * Reads the SAME axis as the list's rail and group badge (provenanceAxis.ts),
 * so the chip and the row it was opened from can never disagree. Three grades,
 * three treatments, and TEXTURE rather than colour is the channel: colour on
 * this surface already belongs to status (the state chip sitting right beside
 * this one) and float, so recolouring for provenance would read as a status
 * change to anyone who has learnt the surface.
 *
 * The chip states the GRADE. The sub-grade and the basis live in the tooltip and,
 * in the open, in ProvenanceNote — density rule 2 from provenanceAxis.ts.
 */
function ProvenanceChips({
  provenance,
  hiddenCount,
  state,
  taskSelected,
  t,
}: {
  provenance: ProvenanceReading;
  hiddenCount: number;
  state: TaskDetailState;
  taskSelected: boolean;
  t: Tokens;
}): ReactElement {
  const chips = headerProvenanceChipsFor({
    origin: provenance.origin,
    hiddenCount,
    state,
    taskSelected,
  });
  return (
    <>
      {chips.grade ? <GradeChip provenance={provenance} t={t} /> : null}
      {chips.hidden > 0 ? <ObservedOnlyChip hiddenCount={chips.hidden} t={t} /> : null}
    </>
  );
}

/**
 * "Observed only" set attempts in this selection aside (designer re-check B1). The
 * record exists, so the chip names the toggle and what it hid. It sits BESIDE the
 * grade chip on a mixed row (fix-C review) and carries no `data-provenance`: it is
 * not a grade, and that attribute stays within the origin enum. Solid border — a
 * known record, not the dashed unknown.
 */
function ObservedOnlyChip({ hiddenCount, t }: { hiddenCount: number; t: Tokens }): ReactElement {
  return (
    <Tooltip
      title={`Observed only is on: ${String(hiddenCount)} reconstructed ${hiddenCount === 1 ? 'attempt is' : 'attempts are'} hidden from this view. Turn it off to see them.`}
    >
      <Box
        data-testid={UI_IDENTIFIERS.Construction.DETAIL_OBSERVED_ONLY_CHIP}
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          px: 0.75,
          py: 0.2,
          borderRadius: 99,
          border: `1px solid ${t.line}`,
          color: t.muted,
          fontFamily: t.mono,
          fontSize: 9.5,
          fontWeight: 700,
          letterSpacing: '0.06em',
          whiteSpace: 'nowrap',
        }}
      >
        {observedOnlyChipLabel(hiddenCount)}
      </Box>
    </Tooltip>
  );
}

function GradeChip({ provenance, t }: { provenance: ProvenanceReading; t: Tokens }): ReactElement {
  const grade = provenanceGradeOf(provenance.origin);
  const reconstructed = grade === 'reconstructed';
  return (
    <Tooltip title={<span style={{ whiteSpace: 'pre-line' }}>{provenance.tooltip}</span>}>
      <Box
        data-provenance={provenance.origin}
        data-testid={UI_IDENTIFIERS.Construction.DETAIL_PROVENANCE_CHIP}
        sx={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 0.4,
          px: 0.75,
          py: 0.2,
          borderRadius: 99,
          border: `1px ${grade === 'unknown' ? 'dashed' : 'solid'} ${t.line}`,
          color: reconstructed ? t.ink : t.muted,
          fontFamily: t.mono,
          fontSize: 9.5,
          fontWeight: 700,
          letterSpacing: '0.06em',
          whiteSpace: 'nowrap',
        }}
      >
        {reconstructed ? (
          // The hatch, in the chip's own ink — the very fill the list draws its
          // rail with, so the two marks read as one material.
          <Box sx={{ width: 6, alignSelf: 'stretch', flexShrink: 0, ...provenanceHatchFill() }} />
        ) : null}
        {reconstructed ? '≈ ' : ''}
        {GRADE_LABEL[grade]}
      </Box>
    </Tooltip>
  );
}

function AttemptSelector({
  attempts,
  t,
  onSelectAttempt,
}: {
  attempts: ReturnType<typeof attemptsForTask>;
  t: Tokens;
  onSelectAttempt: (attempt: number) => void;
}): ReactElement {
  const latestEntry = attempts[attempts.length - 1];
  if (latestEntry === undefined) {
    return (
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.DETAIL_ATTEMPT_SELECT}
        sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, letterSpacing: '0.04em' }}
      >
        NO ATTEMPTS
      </Typography>
    );
  }
  const latest = latestEntry.attempt;
  return (
    <Select
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_ATTEMPT_SELECT}
      inputProps={{ 'aria-label': 'Attempt' }}
      size="small"
      sx={{
        fontFamily: t.mono,
        fontSize: 10.5,
        color: t.ink,
        bgcolor: t.paper,
        '& .MuiOutlinedInput-notchedOutline': { borderColor: t.line },
        '& .MuiSelect-select': { py: 0.15, pl: 1, pr: '24px !important' },
      }}
      value={latest}
      onChange={(e) => {
        onSelectAttempt(e.target.value);
      }}
    >
      {attempts.map((a) => (
        <MenuItem key={a.attemptId} sx={{ fontFamily: t.mono, fontSize: 11 }} value={a.attempt}>
          attempt {a.attempt}
        </MenuItem>
      ))}
    </Select>
  );
}

// ---------------------------------------------------------------------------
// Action bar — invariant across every body. `run` is present and enabled in
// EVERY state (detailActionsFor is the single source of truth for this).
// ---------------------------------------------------------------------------

function ActionBar({
  actions,
  t,
  onAction,
  composer,
  flow,
  reviewOnlyNote,
}: {
  actions: DetailAction[];
  t: Tokens;
  onAction?: ((id: DetailAction['id']) => void) | undefined;
  /** Stage C: the send-back note composer, above the buttons while open. */
  composer?: ReactNode;
  /** Stage C: the decision's line — sending, resumed, or did not land (loud). */
  flow?: FlowNote | undefined;
  /** Why a steer-needed or failed activity offers no action yet (muted). */
  reviewOnlyNote?: string | undefined;
}): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_ACTION_BAR}
      sx={{
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        px: 2,
        py: 1.25,
        borderTop: `1.5px solid ${t.line}`,
        bgcolor: t.paperAlt,
      }}
    >
      {composer}
      {reviewOnlyNote !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_REVIEW_ONLY_NOTE}
          sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.45 }}
        >
          {reviewOnlyNote}
        </Typography>
      ) : null}
      <Box sx={{ display: 'flex', gap: 1 }}>
        {actions.map((a) => {
          const variant = actionVariant(a);
          const button = (
            <Button
              data-reason={a.reason}
              data-testid={UI_IDENTIFIERS.Construction.detailAction(a.id)}
              data-variant={variant}
              disabled={a.disabled}
              size="small"
              startIcon={a.id === 'approve' ? <CheckRoundedIcon /> : undefined}
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 11.5,
                textTransform: 'none',
                ...(variant === 'contained'
                  ? {
                      color: t.bg,
                      bgcolor: t.committedDot,
                      '&:hover': { bgcolor: t.committedFg },
                    }
                  : a.id === 'approve'
                    ? { color: t.committedFg, borderColor: t.committedDot }
                    : variant === 'text'
                      ? { color: t.muted }
                      : { color: t.muted, borderColor: t.line }),
              }}
              variant={variant}
              onClick={() => {
                onAction?.(a.id);
              }}
            >
              {a.label}
            </Button>
          );
          // A disabled button fires no pointer events, so its reason hangs off a
          // wrapper: the operator learns WHY, not just that it is off.
          return a.reason !== undefined ? (
            <Tooltip key={a.id} title={a.reason}>
              <Box component="span" sx={{ display: 'inline-flex' }}>
                {button}
              </Box>
            </Tooltip>
          ) : (
            <Box component="span" key={a.id} sx={{ display: 'inline-flex' }}>
              {button}
            </Box>
          );
        })}
      </Box>
      {flow !== undefined ? (
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_DECISION_FLOW}
          data-tone={flow.tone}
          role="status"
          sx={{
            fontFamily: t.mono,
            fontSize: 11,
            fontWeight: 700,
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
  );
}

/**
 * The decision bar's hierarchy (designer P1-2): a LIVE Approve is the filled
 * primary, Send back is outlined beside it, and Run — disabled with its reason —
 * is a quiet text button, never the loudest thing on a pane that cannot run.
 */
function actionVariant(a: DetailAction): 'contained' | 'outlined' | 'text' {
  if (a.id === 'run') return 'text';
  if (a.id === 'approve' && !a.disabled) return 'contained';
  return 'outlined';
}

/**
 * Send back's note (spec §6): the human's words are what the redraft is for, so
 * the send is off until there are some — typed here, or anchored comments already
 * collected in the co-author rail, which ride along.
 */
function SendBackComposer({
  note,
  anchoredCount,
  caption,
  t,
  onChange,
  onSend,
  onCancel,
}: {
  note: string;
  anchoredCount: number;
  /** What will and will not happen to the note (designer P0-1). */
  caption: string;
  t: Tokens;
  onChange: (note: string) => void;
  onSend: () => void;
  onCancel: () => void;
}): ReactElement {
  const ready = sendBackReady(note, anchoredCount);
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 0.75 }}>
      <Typography sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.muted }}>
        Send back — tell the agent what must change. A note is required
        {anchoredCount > 0
          ? `; ${String(anchoredCount)} anchored comment${anchoredCount === 1 ? '' : 's'} ride along`
          : ''}
        .
      </Typography>
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.DETAIL_DECISION_CAPTION}
        sx={{ fontFamily: t.body, fontSize: 11.5, color: t.awaitingFg, lineHeight: 1.45 }}
      >
        {caption}
      </Typography>
      <TextField
        multiline
        maxRows={6}
        minRows={2}
        placeholder="What must change before this passes?"
        size="small"
        slotProps={{
          htmlInput: {
            'aria-label': 'Send-back note',
            'data-testid': UI_IDENTIFIERS.Construction.DETAIL_DECISION_NOTE,
          },
        }}
        sx={{ bgcolor: t.paper, '& textarea': { fontFamily: t.body, fontSize: 12.5 } }}
        value={note}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
      <Box sx={{ display: 'flex', gap: 1 }}>
        <Button
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_DECISION_SEND_BACK}
          disabled={!ready}
          size="small"
          sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11.5, textTransform: 'none' }}
          variant="contained"
          onClick={onSend}
        >
          Send back with this note
        </Button>
        <Button
          size="small"
          sx={{ fontFamily: t.mono, fontSize: 11.5, textTransform: 'none', color: t.muted }}
          onClick={onCancel}
        >
          Cancel
        </Button>
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The body slot. `detailBodyFor` (bodies/bodyDispatch.ts) owns the choice — pure
// and tested — and this switch is only the wiring from its answer to a renderer.
// ---------------------------------------------------------------------------

function DetailBody(props: {
  row: ConstructionRow | undefined;
  selection: LensSelection;
  selectedAttemptId: string | undefined;
  state: TaskDetailState;
  episodeSlot: DetailPaneProps['episodeSlot'];
  activityTitle: string | undefined;
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  reviewSet: ConstructionReviewSet | undefined;
  hiddenCount: number;
  placement: Placement;
  placementCtx: PlacementViewContext;
  primary: boolean;
}): ReactElement {
  const { row, selection, state, hiddenCount, placement, placementCtx, primary } = props;
  const kind = detailBodyFor(row, selection, state, props.project, primary);
  // A COMPANION (the bare-click summary, a REFERENCE line, an overview-depth
  // absence) rides above whatever body the selection gets; a PRIMARY placement
  // IS the body, framed by its state when nothing has run (§2.1).
  const placed = <ArtifactPlacementView ctx={placementCtx} placement={placement} />;
  const primarySlot = primary ? (
    <ArtifactStateFrame hiddenCount={hiddenCount} row={row} selection={selection} state={state}>
      {placed}
    </ArtifactStateFrame>
  ) : undefined;
  const companion = !primary && placement.kind !== 'none' && kind !== 'absent' ? placed : null;
  const body = <DetailBodySlot {...props} kind={kind} primarySlot={primarySlot} />;
  return companion !== null ? (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
      {companion}
      {body}
    </Box>
  ) : (
    body
  );
}

function DetailBodySlot({
  row,
  selection,
  selectedAttemptId,
  state,
  episodeSlot,
  activityTitle,
  project,
  systemEnvelope,
  reviewSet,
  hiddenCount,
  kind,
  primarySlot,
}: {
  row: ConstructionRow | undefined;
  selection: LensSelection;
  selectedAttemptId: string | undefined;
  state: TaskDetailState;
  episodeSlot: DetailPaneProps['episodeSlot'];
  activityTitle: string | undefined;
  project: ProjectStateWithGit | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  reviewSet: ConstructionReviewSet | undefined;
  hiddenCount: number;
  kind: ReturnType<typeof detailBodyFor>;
  primarySlot: ReactNode;
}): ReactElement {
  switch (kind) {
    case 'absent': {
      const absence = absenceFor(row, selection);
      // `absent` is returned only when absenceFor found one, so this is a
      // narrowing formality rather than a reachable branch.
      return absence !== undefined ? (
        <AbsentBody absence={absence} />
      ) : (
        <UnknownBody hiddenCount={hiddenCount} row={row} selection={selection} state={state} />
      );
    }
    case 'unknown':
      return (
        <UnknownBody hiddenCount={hiddenCount} row={row} selection={selection} state={state} />
      );
    case 'episode': {
      const activityId = selection.activityId;
      const slot =
        episodeSlot !== undefined && activityId !== undefined
          ? episodeSlot({
              activityId,
              ...(selectedAttemptId !== undefined ? { attemptId: selectedAttemptId } : {}),
            })
          : undefined;
      // No slot means this mount has no episode source wired — said plainly
      // rather than rendered as an empty list, which would read as "nothing ever
      // ran" about a task that has a record.
      return slot !== undefined ? (
        <>{slot}</>
      ) : (
        <UnknownBody
          row={row}
          selection={selection}
          statement="A record exists for this. Its episodes are not wired into this mount, so none are shown — that is a gap in this surface, not a statement about whether the work ran."
        />
      );
    }
    case 'review':
      return (
        <ReviewBody
          activityTitle={activityTitle}
          artifactSlot={primarySlot}
          project={project}
          reviewSet={reviewSet}
          row={row}
          selection={selection}
          systemEnvelope={systemEnvelope}
        />
      );
    case 'artifact':
      return (
        <ArtifactBody
          activityTitle={activityTitle}
          primary={primarySlot}
          project={project}
          row={row}
          selection={selection}
          systemEnvelope={systemEnvelope}
        />
      );
  }
}
