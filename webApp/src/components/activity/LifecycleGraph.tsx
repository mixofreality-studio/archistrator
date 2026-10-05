/**
 * The branching stepper of the full-screen Activity Experience: a compact,
 * git-graph-style DAG of ONE activity's internal tasks (Righting Software
 * Appendix A, Figure A-1), mounted in ExperienceChrome's spine bar where the
 * design phases mount SlimSpine. Pure and props-only.
 *
 * It keeps SlimSpine's furniture — small pips on 2px rails, the selected task
 * drawn as the accent pill with its title inline, every other task tooltip-only —
 * and widens it to a graph:
 *
 *   • lanes — a fork peels a branch off the trunk and a join brings it back
 *     (lifecycleGraphLayout.ts decides who gets which lane, lifecycleGraphGeometry.ts
 *     where that is in pixels);
 *   • direction — every rail ends in an arrowhead pointing FORWARD. The one
 *     backward edge is a RETURN ARC from a review over the rail to the task it
 *     sent back, carrying the pair's `↻N` revision count; a review that never
 *     sent anything back has none (backEdgesOf);
 *   • two task kinds, told apart by ICON in a uniform round pip — a robot for an
 *     agentic dispatch, a checked document for a review of its artifact. The icon
 *     says WHAT the task is, so state is carried by the pip's fill and ring, plus a
 *     small corner mark only where colour alone would be ambiguous (✓ done, lock,
 *     ↩ sent back, ! failed);
 *   • revisions — click opens the LATEST; right-click, the ContextMenu key /
 *     Shift+F10, or the caret on the active pill (touch, discoverability) opens a
 *     menu of all of them, newest first — a SHORTCUT to the revision select in the
 *     body — and picking an earlier one selects it for a read-only view;
 *   • lane labels — the branches of a fork are named on their own rails;
 *   • phase labels — a faint mono eyebrow over each phase's first column with its
 *     Table A-1 weight, green and checked once the phase's gate has passed.
 *
 * Hand-rolled SVG under absolutely positioned pips, deliberately not @xyflow: the
 * spine bar needs a fixed-height strip with no pan/zoom, and a DOM pip gets a
 * real focus ring, tooltip and menu anchor for free.
 *
 * ── Every task is reachable, at any width ───────────────────────────────────
 * A forked lifecycle (B12: test plan beside construction, joining at testing) is
 * wider than a narrow window, so the strip owns its scroller and two LENSES:
 *
 *   • scroll (default) — actual size; the scroller scrolls, the edges a task is
 *     scrolled past fade, and the SELECTED task is always scrolled into view
 *     (on mount, on every selection, on every resize);
 *   • fit — every task on screen at once: the strip scaled to the width, or, where
 *     that would shrink a pip below a touch target (MIN_FIT_SCALE), WRAPPED into
 *     one chip per task, grouped under its phase, flowing into rows.
 *
 * The keyboard reaches every task in either lens: each is a tab stop (a locked
 * one too — aria-disabled, never removed from the order), and ←/→/Home/End walk
 * them in reading order, the focus move scrolling the scroller.
 */
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type ReactNode,
} from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import Tooltip from '@mui/material/Tooltip';
import Menu from '@mui/material/Menu';
import MenuItem from '@mui/material/MenuItem';
import ListSubheader from '@mui/material/ListSubheader';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import CheckIcon from '@mui/icons-material/Check';
import LockOutlinedIcon from '@mui/icons-material/LockOutlined';
import UndoIcon from '@mui/icons-material/Undo';
import PriorityHighIcon from '@mui/icons-material/PriorityHigh';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import SmartToyOutlinedIcon from '@mui/icons-material/SmartToyOutlined';
import FactCheckOutlinedIcon from '@mui/icons-material/FactCheckOutlined';
import SwapHorizIcon from '@mui/icons-material/SwapHoriz';
import FitScreenOutlinedIcon from '@mui/icons-material/FitScreenOutlined';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { assertNever } from '../../contracts/exhaustive';
import { layoutLifecycleGraph } from './lifecycleGraphLayout.ts';
import { LIFECYCLE_METRICS, lifecycleGeometry, type ArrowHead } from './lifecycleGraphGeometry.ts';
import {
  backEdgesOf,
  latestRevision,
  revisionLine,
  type LifecycleNode,
  type LifecycleNodeKind,
  type LifecycleNodeState,
  type LifecyclePhase,
  type LifecycleSelection,
} from './lifecycleGraphTypes.ts';

const M = LIFECYCLE_METRICS;
/** Breathing room around the strip so a focus ring / corner mark is never clipped
 *  by the spine bar's horizontal scroller. */
const PAD = 6;

// Pill anatomy, left → right: border, pad, pip, gap, title, gap, caret, pad, border.
const PILL = { border: 1.5, padLeft: 8, titleGap: 8, caretGap: 4, caret: 18, padRight: 5 };
/** The inline `↻N` badge in the pill (fixed, so the pill's width is known up front). */
const BADGE_WIDTH = 24;
/** Mono advance at the pill's 12px bold title (both theme monos sit at ~0.6em). */
const TITLE_CHAR = 7.4;
/** Mono advance of the 8.5px lane label, including its 0.1em tracking. */
const LANE_LABEL_CHAR = 6.2;
/** Mono advance of the 9.5px phase eyebrow, including its 0.12em tracking. */
const EYEBROW_CHAR = 7;

const STATE_LABEL: Record<LifecycleNodeState, string> = {
  done: 'done',
  running: 'agent working',
  awaitingHuman: 'awaiting you',
  sentBack: 'sent back',
  failed: 'failed',
  locked: 'locked',
  pending: 'not started',
};

const KIND_LABEL: Record<LifecycleNodeKind, string> = {
  dispatch: 'Agent task',
  review: 'Review',
};

/** A task progress has REACHED — the rail into it is drawn in the accent. */
function reached(state: LifecycleNodeState): boolean {
  return state !== 'pending' && state !== 'locked';
}

function phaseLabel(p: LifecyclePhase): string {
  const weight = p.weight !== undefined ? ` ${String(p.weight)}%` : '';
  return `${p.passed ? '✓ ' : ''}${p.label.toUpperCase()}${weight}`;
}

interface PipPaint {
  border: string;
  bg: string;
  fg: string;
  /** The kind icon's opacity — a task not reached yet recedes. */
  glyph: number;
}

function pipPaint(t: Tokens, state: LifecycleNodeState): PipPaint {
  switch (state) {
    case 'done':
      return { border: `1.5px solid ${t.line}`, bg: t.committedDot, fg: t.accentText, glyph: 1 };
    case 'running':
      return { border: `1.5px solid ${t.accent}`, bg: 'transparent', fg: t.accent, glyph: 1 };
    case 'awaitingHuman':
      return { border: `2px solid ${t.accent}`, bg: t.awaitingBg, fg: t.accent, glyph: 1 };
    case 'sentBack':
      return {
        border: `1.5px solid ${t.awaitingFg}`,
        bg: t.awaitingBg,
        fg: t.awaitingFg,
        glyph: 1,
      };
    case 'failed':
      return { border: `1.5px solid ${t.dangerFg}`, bg: 'transparent', fg: t.dangerFg, glyph: 1 };
    case 'pending':
      return { border: `1.5px solid ${t.line}`, bg: 'transparent', fg: t.muted, glyph: 0.75 };
    case 'locked':
      return { border: `1.5px solid ${t.line}`, bg: 'transparent', fg: t.muted, glyph: 0.4 };
    default:
      return assertNever(state);
  }
}

/**
 * The corner mark — only for the states colour alone does not settle: done and
 * sent-back share a filled pip with running/awaiting on some themes, and locked
 * vs. not-started is a difference of opacity. Running and awaiting need none: a
 * pulse and an accent ring are unmistakable.
 */
function cornerMark(t: Tokens, state: LifecycleNodeState): { icon: ReactNode; fg: string } | null {
  const sx = { fontSize: 9 };
  switch (state) {
    case 'done':
      return { icon: <CheckIcon sx={sx} />, fg: t.committedText };
    case 'locked':
      return { icon: <LockOutlinedIcon sx={{ fontSize: 8 }} />, fg: t.muted };
    case 'sentBack':
      return { icon: <UndoIcon sx={sx} />, fg: t.awaitingFg };
    case 'failed':
      return { icon: <PriorityHighIcon sx={sx} />, fg: t.dangerFg };
    case 'running':
    case 'awaitingHuman':
    case 'pending':
      return null;
    default:
      return assertNever(state);
  }
}

function arrowPath(a: ArrowHead): string {
  const [l, h] = [M.arrowLen, M.arrowHalf];
  return a.dir === 'right'
    ? `M${String(a.x)} ${String(a.y)} l${String(-l)} ${String(-h)} v${String(2 * h)} Z`
    : `M${String(a.x)} ${String(a.y)} l${String(-h)} ${String(-l)} h${String(2 * h)} Z`;
}

/** The props that make an element a task's button (the pip, the pill, a chip). */
interface NodeButtonProps {
  'aria-haspopup': 'menu';
  'aria-label': string;
  'aria-current': 'step' | undefined;
  'aria-disabled': true | undefined;
  'data-kind': LifecycleNodeKind;
  'data-state': LifecycleNodeState;
  'data-testid': string;
  role: 'button';
  tabIndex: number;
  onClick: () => void;
  onContextMenu: (e: MouseEvent<HTMLElement>) => void;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
}

/** How the strip meets a spine bar narrower than itself. */
export type LifecycleLens = 'scroll' | 'fit';

/**
 * The lens outlives the screen: it is a reader's preference about the strip, not
 * a fact about one activity, and the Activity Experience remounts the graph on
 * every activity it opens (module memory, as the design views do).
 */
let rememberedLens: LifecycleLens = 'scroll';

/**
 * Below this scale the fitted strip's pips (24px) shrink under ~14px — too small
 * to hit on a touch screen — so the fit lens WRAPS instead: every task as a chip,
 * phase by phase, in rows the width decides.
 */
const MIN_FIT_SCALE = 0.6;
/** Width of the edge fades that say "more tasks this way" in the scroll lens. */
const FADE = 28;
/** Clear space kept between the selected task and the scroller's edge. */
const IN_VIEW_MARGIN = 24;

export interface LifecycleGraphProps {
  /** The activity's tasks in AUTHORED order — the first-authored chain is the trunk. */
  nodes: readonly LifecycleNode[];
  /** Phases in display order; a phase with no task is skipped. */
  phases: readonly LifecyclePhase[];
  selected: LifecycleSelection;
  /**
   * Click / Enter / Space passes the node's latest revision with `picked` false —
   * the owner may carry the revision being read across a draft↔review pair
   * (revisionOnNavigate). The revision menu passes the chosen one, `picked` true.
   */
  onSelect: (nodeId: string, revision: number, picked: boolean) => void;
  /** The colour behind the strip (pips mask the rails with it). Defaults to the
   *  spine bar's `paperAlt`. */
  surface?: string | undefined;
}

export function LifecycleGraph({
  nodes,
  phases,
  selected,
  onSelect,
  surface,
}: LifecycleGraphProps): ReactNode {
  const t = useTokens();
  const bgSurface = surface ?? t.paperAlt;
  const [menu, setMenu] = useState<{ nodeId: string; anchor: HTMLElement } | null>(null);
  const [lens, setLens] = useState<LifecycleLens>(rememberedLens);
  const chooseLens = (next: LifecycleLens): void => {
    rememberedLens = next;
    setLens(next);
  };
  const rootRef = useRef<HTMLDivElement | null>(null);
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  /** The scroller's width — what the fit lens fits into. Null until measured. */
  const [avail, setAvail] = useState<number | null>(null);
  /** Which edges have tasks scrolled past them (the scroll lens's fades). */
  const [hidden, setHidden] = useState({ left: false, right: false });

  const activeNode = nodes.find((n) => n.id === selected.nodeId);
  const activeLatest = activeNode === undefined ? 0 : latestRevision(activeNode);
  const historical = activeNode !== undefined && selected.revision < activeLatest;
  // The pill says which revision it is showing only when that is NOT the latest.
  const activeTitle =
    activeNode === undefined
      ? ''
      : historical
        ? `${activeNode.title} · rev ${String(selected.revision)}/${String(activeLatest)}`
        : activeNode.title;
  const titleWidth = Math.ceil(activeTitle.length * TITLE_CHAR);
  const activeBadge =
    activeNode !== undefined && activeNode.revisions.length > 1 ? PILL.titleGap + BADGE_WIDTH : 0;

  const layout = useMemo(() => layoutLifecycleGraph(nodes), [nodes]);
  const backEdges = useMemo(() => backEdgesOf(nodes), [nodes]);
  const geometry = useMemo(
    () =>
      lifecycleGeometry(
        layout,
        nodes.map((n) => ({
          id: n.id,
          phase: n.phase,
          laneLabelWidth:
            n.laneLabel !== undefined ? Math.ceil(n.laneLabel.length * LANE_LABEL_CHAR) : 0,
        })),
        phases.map((p) => ({ id: p.id, labelWidth: phaseLabel(p).length * EYEBROW_CHAR + 4 })),
        activeNode === undefined
          ? undefined
          : {
              nodeId: activeNode.id,
              left: M.pip / 2 + PILL.padLeft + PILL.border,
              right:
                M.pip / 2 +
                PILL.titleGap +
                titleWidth +
                activeBadge +
                PILL.caretGap +
                PILL.caret +
                PILL.padRight +
                PILL.border,
            },
        M,
        backEdges
      ),
    [layout, nodes, phases, activeNode, titleWidth, activeBadge, backEdges]
  );

  // The strip's full box, padding included — what the scroll lens scrolls over and
  // the fit lens scales into the scroller.
  const naturalWidth = geometry.width + 2 * PAD;
  const naturalHeight = geometry.height + 2 * PAD;
  const scale = avail === null || avail <= 0 ? 1 : Math.min(1, avail / naturalWidth);
  const form: 'actual' | 'scaled' | 'wrap' =
    lens === 'scroll' ? 'actual' : scale >= MIN_FIT_SCALE ? 'scaled' : 'wrap';

  /** Every task in reading order — column, then lane — for the arrow keys and the wrap. */
  const readingOrder = useMemo(
    () =>
      [...nodes].sort((a, b) => {
        const pa = layout.positions.get(a.id);
        const pb = layout.positions.get(b.id);
        return (pa?.col ?? 0) - (pb?.col ?? 0) || (pa?.lane ?? 0) - (pb?.lane ?? 0);
      }),
    [nodes, layout]
  );

  const measure = useCallback((): void => {
    const sc = scrollerRef.current;
    if (sc === null) return;
    setAvail((prev) => (prev === sc.clientWidth ? prev : sc.clientWidth));
    const left = sc.scrollLeft > 1;
    const right = sc.scrollLeft + sc.clientWidth < sc.scrollWidth - 1;
    setHidden((prev) => (prev.left === left && prev.right === right ? prev : { left, right }));
  }, []);

  useEffect(() => {
    const sc = scrollerRef.current;
    if (sc === null) return undefined;
    measure();
    if (typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(measure);
    ro.observe(sc);
    return (): void => {
      ro.disconnect();
    };
  }, [measure]);

  // The selected task is ALWAYS on screen: whenever the selection, the lens or the
  // room changes, a selection the scroller has scrolled past (or that sits past
  // its right edge on first paint) is brought to the middle.
  useLayoutEffect(() => {
    const sc = scrollerRef.current;
    if (sc === null) return;
    if (form === 'actual') {
      const el = sc.querySelector<HTMLElement>('[data-lifecycle-active="true"]');
      if (el !== null) {
        const s = sc.getBoundingClientRect();
        const e = el.getBoundingClientRect();
        if (e.left < s.left + IN_VIEW_MARGIN || e.right > s.right - IN_VIEW_MARGIN) {
          sc.scrollLeft += e.left + e.width / 2 - (s.left + s.width / 2);
        }
      }
    } else {
      sc.scrollLeft = 0;
    }
    measure();
  }, [selected.nodeId, selected.revision, form, avail, naturalWidth, measure]);

  const stateOf = new Map(nodes.map((n) => [n.id, n.state]));
  const railReached = (from: string, to: string): boolean =>
    stateOf.get(from) === 'done' && reached(stateOf.get(to) ?? 'pending');
  // Accent rails last, so where a finished trunk and an unstarted branch share a
  // run the finished one is what shows.
  const rails = [...geometry.rails].sort(
    (a, b) => Number(railReached(a.from, a.to)) - Number(railReached(b.from, b.to))
  );
  // A return arc is LIVE (accent) while the send-back is still being worked off —
  // the review sits in `sentBack` — and history (muted) once the pair moved on.
  const arcColour = (reviewId: string): string =>
    stateOf.get(reviewId) === 'sentBack' ? t.awaitingFg : t.muted;

  const menuNode = menu === null ? undefined : nodes.find((n) => n.id === menu.nodeId);
  const openMenu = (nodeId: string, anchor: HTMLElement): void => {
    setMenu({ nodeId, anchor });
  };

  /** A phase is CURRENT while one of its tasks is reached but not done. */
  const phaseColour = (phase: LifecyclePhase): { colour: string; current: boolean } => {
    const current = nodes.some(
      (n) => n.phase === phase.id && reached(n.state) && n.state !== 'done'
    );
    return { colour: phase.passed ? t.committedText : current ? t.ink : t.muted, current };
  };

  /** Arrow keys walk the tasks in reading order (Home / End jump to the ends); the
   *  focus move scrolls the scroller, so every task is reachable from the keyboard. */
  const focusNeighbour = (fromId: string, key: string): boolean => {
    const i = readingOrder.findIndex((n) => n.id === fromId);
    const last = readingOrder.length - 1;
    const to =
      key === 'ArrowRight'
        ? Math.min(last, i + 1)
        : key === 'ArrowLeft'
          ? Math.max(0, i - 1)
          : key === 'Home'
            ? 0
            : key === 'End'
              ? last
              : -1;
    if (to < 0) return false;
    const target = readingOrder[to];
    if (target === undefined) return true;
    rootRef.current
      ?.querySelector<HTMLElement>(
        `[data-testid="${CSS.escape(UI_IDENTIFIERS.ActivityLifecycle.node(target.id))}"]`
      )
      ?.focus();
    return true;
  };

  const focusRing = {
    outline: 'none',
    '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: '2px' },
  };

  const pipFor = (n: LifecycleNode): ReactNode => {
    const paint = pipPaint(t, n.state);
    const mark = cornerMark(t, n.state);
    const KindIcon = n.kind === 'review' ? FactCheckOutlinedIcon : SmartToyOutlinedIcon;
    return (
      <Box sx={{ position: 'relative', display: 'flex', flexShrink: 0 }}>
        {n.state === 'running' ? (
          <Box
            sx={{
              position: 'absolute',
              inset: -3,
              borderRadius: '50%',
              border: `1.5px solid ${t.accent}`,
              animation: 'lifecyclePulse 1.6s ease-out infinite',
              '@keyframes lifecyclePulse': {
                from: { opacity: 0.7, transform: 'scale(0.85)' },
                to: { opacity: 0, transform: 'scale(1.2)' },
              },
              '@media (prefers-reduced-motion: reduce)': { animation: 'none', opacity: 0.4 },
            }}
          />
        ) : null}
        <Box
          sx={{
            width: M.pip,
            height: M.pip,
            flexShrink: 0,
            boxSizing: 'border-box',
            borderRadius: '50%',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            border: paint.border,
            color: paint.fg,
            // An opaque surface under the (possibly translucent) state fill, so
            // the rails stop AT the pip instead of showing through it — which is
            // also why a task not reached yet dims its glyph and never the pip.
            backgroundColor: bgSurface,
            backgroundImage: `linear-gradient(${paint.bg}, ${paint.bg})`,
          }}
        >
          <KindIcon sx={{ fontSize: 14, opacity: paint.glyph }} />
        </Box>
        {mark !== null ? (
          <Box
            sx={{
              position: 'absolute',
              right: -4,
              bottom: -4,
              width: 12,
              height: 12,
              boxSizing: 'border-box',
              borderRadius: '50%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              bgcolor: bgSurface,
              border: `1px solid ${mark.fg}`,
              color: mark.fg,
            }}
          >
            {mark.icon}
          </Box>
        ) : null}
      </Box>
    );
  };

  /** What makes an element THE task's button: label, state, selection, menu, keys. */
  const buttonPropsFor = (n: LifecycleNode): NodeButtonProps => {
    const active = n.id === selected.nodeId;
    const locked = n.state === 'locked' && !active;
    const latest = latestRevision(n);
    const hasMenu = n.revisions.length > 0;
    const revisions = n.revisions.length > 1 ? `, ${String(n.revisions.length)} revisions` : '';
    return {
      'aria-haspopup': 'menu' as const,
      'aria-label': `${KIND_LABEL[n.kind]}: ${n.title} — ${STATE_LABEL[n.state]}${revisions}`,
      'aria-current': active ? ('step' as const) : undefined,
      'aria-disabled': locked ? true : undefined,
      'data-kind': n.kind,
      'data-state': n.state,
      'data-testid': UI_IDENTIFIERS.ActivityLifecycle.node(n.id),
      role: 'button' as const,
      // A locked task stays FOCUSABLE (aria-disabled, not removed from the tab
      // order): the keyboard reaches every task, and its label says why it waits.
      tabIndex: 0,
      onClick: (): void => {
        if (!locked) onSelect(n.id, latest, false);
      },
      onContextMenu: (e: MouseEvent<HTMLElement>): void => {
        // Always swallow the native menu over a pip — a browser menu here reads
        // as the feature being broken, even on a task with no history yet.
        e.preventDefault();
        if (!locked && hasMenu) openMenu(n.id, e.currentTarget);
      },
      onKeyDown: (e: KeyboardEvent<HTMLElement>): void => {
        if (focusNeighbour(n.id, e.key)) {
          e.preventDefault();
          return;
        }
        if (locked) return;
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onSelect(n.id, latest, false);
        } else if (hasMenu && (e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10'))) {
          e.preventDefault();
          openMenu(n.id, e.currentTarget);
        }
      },
    };
  };

  /** The active task's caret: the revision menu for touch, and for discoverability. */
  const caretFor = (n: LifecycleNode): ReactNode => {
    const hasMenu = n.revisions.length > 0;
    return (
      <Tooltip title={hasMenu ? 'Revisions' : 'No revisions yet'}>
        <Box
          aria-disabled={hasMenu ? undefined : true}
          aria-haspopup="menu"
          aria-label={`Revisions of ${n.title}`}
          data-testid={UI_IDENTIFIERS.ActivityLifecycle.nodeMenuButton(n.id)}
          role="button"
          sx={{
            ml: `${String(PILL.caretGap)}px`,
            width: PILL.caret,
            height: PILL.caret,
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            borderRadius: '50%',
            color: t.awaitingFg,
            opacity: hasMenu ? 1 : 0.4,
            cursor: hasMenu ? 'pointer' : 'default',
            '&:hover': hasMenu ? { bgcolor: t.awaitingBg } : {},
            ...focusRing,
          }}
          tabIndex={hasMenu ? 0 : -1}
          onClick={(e) => {
            if (hasMenu) openMenu(n.id, e.currentTarget);
          }}
          onKeyDown={(e) => {
            if (hasMenu && (e.key === 'Enter' || e.key === ' ')) {
              e.preventDefault();
              openMenu(n.id, e.currentTarget);
            }
          }}
        >
          <ExpandMoreIcon sx={{ fontSize: 16 }} />
        </Box>
      </Tooltip>
    );
  };

  const strip = (
    <Box
      aria-label="Activity lifecycle"
      data-testid={UI_IDENTIFIERS.ActivityLifecycle.GRAPH}
      role="group"
      sx={{
        position: 'relative',
        flexShrink: 0,
        boxSizing: 'content-box',
        width: geometry.width,
        height: geometry.height,
        p: `${String(PAD)}px`,
      }}
    >
      {/* phase labels — text only, over (or under) the phase's first column */}
      {geometry.phases.map((box) => {
        const phase = phases.find((p) => p.id === box.id);
        if (phase === undefined) return null;
        const { colour, current } = phaseColour(phase);
        return (
          <Typography
            data-testid={UI_IDENTIFIERS.ActivityLifecycle.phase(phase.id)}
            key={phase.id}
            sx={{
              position: 'absolute',
              left: PAD + box.x,
              top:
                PAD +
                (box.side === 'top'
                  ? (geometry.topRows - 1 - box.row) * M.labelRow
                  : geometry.railsTop + geometry.railsHeight + box.row * M.labelRow + 3),
              fontFamily: t.mono,
              fontSize: 9.5,
              lineHeight: '11px',
              letterSpacing: '0.12em',
              whiteSpace: 'nowrap',
              fontWeight: current ? 700 : 400,
              color: colour,
              pointerEvents: 'none',
            }}
          >
            {phaseLabel(phase)}
          </Typography>
        );
      })}

      {/* rails, arrowheads and return arcs */}
      <Box
        aria-hidden
        component="svg"
        sx={{
          position: 'absolute',
          left: PAD,
          top: PAD + geometry.railsTop,
          width: geometry.width,
          height: geometry.railsHeight,
          overflow: 'visible',
          pointerEvents: 'none',
        }}
      >
        {rails.map((r) => {
          const colour = railReached(r.from, r.to) ? t.accent : t.line;
          return (
            <g key={`${r.from}>${r.to}`}>
              <path d={r.d} fill="none" stroke={colour} strokeLinecap="butt" strokeWidth={2} />
              <path d={arrowPath(r.arrow)} fill={colour} />
            </g>
          );
        })}
        {geometry.backArcs.map((a) => (
          <g data-back-edge={`${a.from}>${a.to}`} key={`${a.from}<${a.to}`}>
            <path d={a.d} fill="none" stroke={arcColour(a.from)} strokeWidth={1.5} />
            <path d={arrowPath(a.arrow)} fill={arcColour(a.from)} />
          </g>
        ))}
      </Box>

      {/* the `↻N` each return arc carries on its crown */}
      {geometry.backArcs.map((a) => {
        const edge = backEdges.find((b) => b.from === a.from && b.to === a.to);
        if (edge === undefined) return null;
        return (
          <Box
            key={`${a.from}<${a.to}`}
            sx={{
              position: 'absolute',
              left: PAD + a.badge.x - M.arcBadge.w / 2,
              top: PAD + geometry.railsTop + a.badge.y - M.arcBadge.h / 2,
              width: M.arcBadge.w,
              height: M.arcBadge.h,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              bgcolor: bgSurface,
              color: arcColour(a.from),
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 8.5,
              lineHeight: 1,
              whiteSpace: 'nowrap',
              pointerEvents: 'none',
            }}
          >
            {`↻${String(edge.revisions)}`}
          </Box>
        );
      })}

      {/* lane labels — each on its own rail, masking it, in the zone the geometry
          reserved ahead of the branch's first pip */}
      {geometry.laneLabels.map((box) => {
        const node = nodes.find((n) => n.id === box.nodeId);
        if (node?.laneLabel === undefined) return null;
        return (
          <Typography
            data-testid={UI_IDENTIFIERS.ActivityLifecycle.laneLabel(node.id)}
            key={node.id}
            sx={{
              position: 'absolute',
              left: PAD + box.x - 3,
              top: PAD + geometry.railsTop + box.y,
              width: box.width + 6,
              height: box.height,
              boxSizing: 'border-box',
              px: '3px',
              bgcolor: bgSurface,
              fontFamily: t.mono,
              fontSize: 8.5,
              lineHeight: `${String(box.height)}px`,
              letterSpacing: '0.1em',
              textTransform: 'uppercase',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              color: t.muted,
              pointerEvents: 'none',
            }}
          >
            {node.laneLabel}
          </Typography>
        );
      })}

      {/* pips */}
      {nodes.map((n) => {
        const pos = layout.positions.get(n.id);
        const cx = geometry.x.get(n.id);
        if (pos === undefined || cx === undefined) return null;
        const cy = geometry.railsTop + geometry.laneY(pos.lane);
        const active = n.id === selected.nodeId;
        const locked = n.state === 'locked' && !active;

        if (active) {
          return (
            <Box
              data-lifecycle-active="true"
              key={n.id}
              sx={{
                position: 'absolute',
                left: PAD + cx - M.pip / 2 - PILL.padLeft - PILL.border,
                top: PAD + cy,
                transform: 'translateY(-50%)',
                display: 'flex',
                alignItems: 'center',
                boxSizing: 'border-box',
                pl: `${String(PILL.padLeft)}px`,
                pr: `${String(PILL.padRight)}px`,
                py: 0.4,
                borderRadius: 99,
                border: `${String(PILL.border)}px solid ${t.accent}`,
                // Same opaque-under-translucent layering as the pips: awaitingBg is
                // an rgba() on the dark themes and a rail must not show through.
                backgroundColor: bgSurface,
                backgroundImage: `linear-gradient(${t.awaitingBg}, ${t.awaitingBg})`,
                zIndex: 1,
              }}
            >
              <Box
                {...buttonPropsFor(n)}
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: `${String(PILL.titleGap)}px`,
                  cursor: 'pointer',
                  borderRadius: 99,
                  ...focusRing,
                }}
              >
                {pipFor(n)}
                <Typography
                  sx={{
                    width: titleWidth,
                    fontFamily: t.mono,
                    fontWeight: 700,
                    fontSize: 12,
                    color: t.awaitingFg,
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                  }}
                >
                  {activeTitle}
                </Typography>
                {/* `↻N` — how many revisions THIS task has; the pair's count rides
                    its return arc, but the pill is where the selection's own lives. */}
                {n.revisions.length > 1 ? (
                  <Box
                    sx={{
                      flexShrink: 0,
                      boxSizing: 'border-box',
                      width: BADGE_WIDTH,
                      borderRadius: '6px',
                      border: `1px solid ${t.awaitingFg}`,
                      color: t.awaitingFg,
                      fontFamily: t.mono,
                      fontWeight: 700,
                      fontSize: 8.5,
                      lineHeight: '11px',
                      textAlign: 'center',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {`↻${String(n.revisions.length)}`}
                  </Box>
                ) : null}
              </Box>
              {caretFor(n)}
            </Box>
          );
        }

        return (
          // Keyed by the selection as well (SlimSpine UX-P1-6): a navigation
          // remounts every tooltip, dismissing one left open by the hover that
          // preceded the click.
          <Tooltip
            key={`${n.id}-${selected.nodeId}`}
            title={`${KIND_LABEL[n.kind]}: ${n.title} — ${STATE_LABEL[n.state]}`}
          >
            <Box
              {...buttonPropsFor(n)}
              sx={{
                position: 'absolute',
                left: PAD + cx,
                top: PAD + cy,
                transform: 'translate(-50%, -50%)',
                display: 'flex',
                cursor: locked ? 'not-allowed' : 'pointer',
                borderRadius: '50%',
                ...focusRing,
              }}
            >
              {pipFor(n)}
            </Box>
          </Tooltip>
        );
      })}
    </Box>
  );

  // The wrapped form: no rails, so the phases carry the structure — each phase's
  // tasks in reading order under its eyebrow, the groups flowing into rows.
  const groups = [
    ...phases.map((p) => ({ phase: p, id: p.id })),
    { phase: undefined, id: '' },
  ].flatMap((g) => {
    const members = readingOrder.filter((n) =>
      g.phase === undefined ? !phases.some((p) => p.id === n.phase) : n.phase === g.id
    );
    return members.length > 0 ? [{ ...g, members }] : [];
  });
  const wrap = (
    <Box
      aria-label="Activity lifecycle"
      data-testid={UI_IDENTIFIERS.ActivityLifecycle.WRAP}
      role="group"
      sx={{
        display: 'flex',
        flexWrap: 'wrap',
        columnGap: 2,
        rowGap: 1,
        p: `${String(PAD)}px`,
        minWidth: 0,
      }}
    >
      {groups.map((g) => {
        const paint = g.phase === undefined ? undefined : phaseColour(g.phase);
        return (
          <Box
            key={g.id}
            sx={{
              display: 'flex',
              flexDirection: 'column',
              gap: 0.5,
              minWidth: 0,
              maxWidth: '100%',
            }}
          >
            {g.phase !== undefined && paint !== undefined ? (
              <Typography
                data-testid={UI_IDENTIFIERS.ActivityLifecycle.phase(g.phase.id)}
                sx={{
                  fontFamily: t.mono,
                  fontSize: 9.5,
                  lineHeight: '11px',
                  letterSpacing: '0.12em',
                  whiteSpace: 'nowrap',
                  fontWeight: paint.current ? 700 : 400,
                  color: paint.colour,
                }}
              >
                {phaseLabel(g.phase)}
              </Typography>
            ) : null}
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.75 }}>
              {g.members.map((n) => {
                const active = n.id === selected.nodeId;
                const locked = n.state === 'locked' && !active;
                return (
                  <Box
                    data-lifecycle-active={active ? 'true' : undefined}
                    key={n.id}
                    sx={{
                      display: 'flex',
                      alignItems: 'center',
                      maxWidth: '100%',
                      boxSizing: 'border-box',
                      pl: '3px',
                      pr: active ? `${String(PILL.padRight)}px` : '10px',
                      py: '2px',
                      borderRadius: 99,
                      border: `${String(PILL.border)}px solid ${active ? t.accent : t.line}`,
                      backgroundColor: bgSurface,
                      backgroundImage: active
                        ? `linear-gradient(${t.awaitingBg}, ${t.awaitingBg})`
                        : 'none',
                    }}
                  >
                    <Box
                      {...buttonPropsFor(n)}
                      sx={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: '6px',
                        minWidth: 0,
                        cursor: locked ? 'not-allowed' : 'pointer',
                        borderRadius: 99,
                        ...focusRing,
                      }}
                    >
                      {pipFor(n)}
                      <Typography
                        sx={{
                          fontFamily: t.mono,
                          fontWeight: active ? 700 : 400,
                          fontSize: 11,
                          color: active ? t.awaitingFg : locked ? t.muted : t.ink,
                          whiteSpace: 'nowrap',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                        }}
                      >
                        {active ? activeTitle : n.title}
                      </Typography>
                    </Box>
                    {active ? caretFor(n) : null}
                  </Box>
                );
              })}
            </Box>
          </Box>
        );
      })}
    </Box>
  );

  // The scroll lens fades the edge tasks are scrolled past, so a strip wider than
  // the bar never reads as complete (macOS hides the scrollbar itself).
  const fadeMask =
    form === 'actual' && (hidden.left || hidden.right)
      ? `linear-gradient(to right, ${hidden.left ? 'transparent' : '#000'} 0, #000 ${String(FADE)}px, #000 calc(100% - ${String(FADE)}px), ${hidden.right ? 'transparent' : '#000'} 100%)`
      : undefined;

  return (
    <Box
      ref={rootRef}
      sx={{ display: 'flex', alignItems: 'center', gap: 1, width: '100%', minWidth: 0 }}
    >
      <Box
        data-lens={lens}
        data-testid={UI_IDENTIFIERS.ActivityLifecycle.SCROLLER}
        ref={scrollerRef}
        sx={{
          flexGrow: 1,
          minWidth: 0,
          overflowX: form === 'actual' ? 'auto' : 'hidden',
          overflowY: 'hidden',
          scrollbarWidth: 'thin',
          ...(fadeMask !== undefined ? { maskImage: fadeMask, WebkitMaskImage: fadeMask } : {}),
        }}
        onScroll={measure}
      >
        {form === 'wrap' ? (
          wrap
        ) : form === 'scaled' ? (
          // Scaled from the top-left corner into a box of the scaled size, so the
          // bar's layout sees what is painted; pips stay buttons at any scale.
          <Box sx={{ width: naturalWidth * scale, height: naturalHeight * scale }}>
            <Box
              sx={{
                width: naturalWidth,
                transform: `scale(${String(scale)})`,
                transformOrigin: '0 0',
              }}
            >
              {strip}
            </Box>
          </Box>
        ) : (
          strip
        )}
      </Box>

      <ToggleButtonGroup
        exclusive
        aria-label="Lifecycle lens"
        size="small"
        sx={{ flexShrink: 0, alignSelf: 'flex-start', mt: '4px' }}
        value={lens}
        onChange={(_e, next: LifecycleLens | null) => {
          if (next !== null) chooseLens(next);
        }}
      >
        <ToggleButton
          aria-label="Actual size, scrolling"
          data-testid={UI_IDENTIFIERS.ActivityLifecycle.LENS_SCROLL}
          sx={{ p: '3px', color: t.muted, borderColor: t.line }}
          title="Actual size — scroll to every task"
          value="scroll"
        >
          <SwapHorizIcon sx={{ fontSize: 16 }} />
        </ToggleButton>
        <ToggleButton
          aria-label="Fit every task on screen"
          data-testid={UI_IDENTIFIERS.ActivityLifecycle.LENS_FIT}
          sx={{ p: '3px', color: t.muted, borderColor: t.line }}
          title="Fit every task on screen"
          value="fit"
        >
          <FitScreenOutlinedIcon sx={{ fontSize: 16 }} />
        </ToggleButton>
      </ToggleButtonGroup>

      <Menu
        anchorEl={menu?.anchor ?? null}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
        open={menu !== null && menuNode !== undefined}
        slotProps={{
          list: {
            'aria-label': menuNode === undefined ? 'Revisions' : `Revisions of ${menuNode.title}`,
            dense: true,
          },
          paper: { 'data-testid': UI_IDENTIFIERS.ActivityLifecycle.REVISION_MENU } as object,
        }}
        onClose={() => {
          setMenu(null);
        }}
      >
        {menuNode !== undefined ? (
          <ListSubheader
            sx={{
              fontFamily: t.mono,
              fontSize: 10,
              letterSpacing: '0.14em',
              lineHeight: '26px',
              color: t.muted,
              bgcolor: 'transparent',
            }}
          >
            {`${menuNode.title.toUpperCase()} · REVISIONS`}
          </ListSubheader>
        ) : null}
        {menuNode !== undefined
          ? [...menuNode.revisions]
              .sort((a, b) => b.n - a.n)
              .map((rev) => {
                const isLatest = rev.n === latestRevision(menuNode);
                const isShown = menuNode.id === selected.nodeId && rev.n === selected.revision;
                return (
                  <MenuItem
                    data-testid={UI_IDENTIFIERS.ActivityLifecycle.revisionItem(menuNode.id, rev.n)}
                    key={rev.n}
                    selected={isShown}
                    sx={{ fontFamily: t.mono, fontSize: 12, color: isLatest ? t.ink : t.muted }}
                    onClick={() => {
                      setMenu(null);
                      onSelect(menuNode.id, rev.n, true);
                    }}
                  >
                    {revisionLine(rev, isLatest)}
                  </MenuItem>
                );
              })
          : null}
      </Menu>
    </Box>
  );
}
