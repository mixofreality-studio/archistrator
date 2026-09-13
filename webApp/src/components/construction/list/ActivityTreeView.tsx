/* eslint-disable react-refresh/only-export-components -- the pure presentation rules are
   re-exported alongside the component, the same colocation status.tsx / KindBadge.tsx /
   DetailPane.tsx already use. */
/**
 * The LIST lens — activity › lifecycle phase › Figure A-1 task, in one tree.
 *
 * The derivation is NOT here: `buildActivityTree` (activityTree.ts) already
 * decided every row this renders, and the channel rules live in
 * activityRowPresentation.ts. This file is the geometry — what each fact looks
 * like — and nothing else, which is why the two modules beside it are plain
 * `.ts` and carry the tests.
 *
 * THE ONE RULE THIS SURFACE OBEYS
 * -------------------------------
 * Two prototype rounds were rejected for visual density, and the diagnosis was:
 *
 *     every magnitude gets a geometric channel;
 *     nothing with a geometry gets a chip.
 *
 * So: effort is a bar LENGTH, float is a rail band PLUS a numeral (WCAG 1.4.1 —
 * NetworkNode.tsx already sets that precedent), criticality is a border WEIGHT
 * (2→3px, never a "CRITICAL" chip), phase weight is a segment WIDTH, progress is
 * a fill EXTENT, and retries are a `↻N` numeral. Only STATES get chips, at most
 * one per row, and `unknown` gets none at all — chip-less IS the signal, and
 * unknown is the resting state of every task nobody has run yet. A screen
 * of "UNKNOWN" chips is the failure that got the earlier rounds rejected.
 *
 * THE SECOND AXIS
 * ---------------
 * State answers "what happened?". PROVENANCE answers "how do we know?", and the
 * two are orthogonal — every row carries both. It is drawn as a TEXTURE (a
 * hatched leading rail) and never as a colour, because colour here is already
 * spoken for by status and float. The badge that names it rides GROUP headers
 * only. All of it lives in ../provenance.tsx; this file just hangs the rail off
 * the leading edge of each tier so an expanded reconstructed activity reads as
 * one continuous hatched band rather than fourteen separate annotations.
 *
 * WHY THREE TIERS STILL READ AS TWO
 * ---------------------------------
 * Tier 2 is a group RULE, not a card:
 *
 *     ┌ CONSTRUCTION ····· wt 40 · exit: Construction is code-complete and self-verified
 *
 * A rule recedes; a card asserts. Three levels of card would read as three
 * levels of nesting and put the tasks — the things an operator actually acts on
 * — three boxes deep. Only tier 1 is a row with a background.
 *
 * WHY RichTreeView AND NOT A HAND-ROLLED LIST
 * -------------------------------------------
 * The tree is DERIVED, so a data-driven `items` prop is the honest shape: the
 * component owns roving-tabindex keyboard traversal, `role="tree"`/`treeitem`
 * semantics and aria-expanded, none of which a hand-rolled `<div>` list gets for
 * free. `slots.item` then hands the whole row body back to us, so MUI owns the
 * accessibility and this file owns every pixel. The `apiRef` is wired now
 * because Task 11's search-reveal and expand-to-current-phase drive it; nothing
 * here calls it yet.
 *
 * `expansionTrigger: 'iconContainer'` is deliberate — clicking a ROW selects it
 * (and fills the detail pane); only the chevron expands. Sharing one gesture
 * between "show me this" and "show me what is inside this" is how an operator
 * loses their place.
 *
 * NAVIGABILITY (Stage B Task 11)
 * -------------------------------
 * `searchQuery` and `expandToCurrentPhaseSignal` are the two props that drive
 * the apiRef: a search match's ancestors are expanded and the match is
 * focused (which also scrolls it into view) via `apiRef.current.focusItem`;
 * "Expand to current phase" additively expands every in-flight activity via
 * the same imperative surface. Both reuse the pure predicates in
 * activityScope.ts — this file owns the choreography, not the rules.
 *
 * THE PROVENANCE GUARANTEE, CARRIED FORWARD FROM TASK 7
 * -------------------------------------------------------
 * The `≈ RECONSTRUCTED` badge rides group headers (tier 1/2) only — a task row
 * in isolation reads `SRS ✓ PASSED` with no mark of its own, and the design's
 * defence is that its group header is always visible above it (an argument
 * from CONTEXT). Search is the one feature that can break that argument: it
 * can reveal and focus a tier-3 row on its own initiative. So a row this file
 * marks as a search MATCH also carries its own provenance mark inline
 * (`SearchMatchProvenance` below) whenever that task is not `recorded` —
 * independent of whether its ancestors happen to still be on screen. The
 * ancestor auto-expand is real and reduces how often that mark is even needed,
 * but it is not what the guarantee rests on.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactElement,
  type SyntheticEvent,
} from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Collapse from '@mui/material/Collapse';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import { alpha } from '@mui/material/styles';
import ChevronRightRoundedIcon from '@mui/icons-material/ChevronRightRounded';
import ExpandMoreRoundedIcon from '@mui/icons-material/ExpandMoreRounded';
import { RichTreeView } from '@mui/x-tree-view/RichTreeView';
import { useRichTreeViewApiRef, useTreeItemModel } from '@mui/x-tree-view/hooks';
import { useTreeItem } from '@mui/x-tree-view/useTreeItem';
import { TreeItemProvider } from '@mui/x-tree-view/TreeItemProvider';
import type { TreeItemProps } from '@mui/x-tree-view/TreeItem';

import type { TaskAttemptRow } from '../../../contracts/types';
import type { FloatBand } from '../../../contracts/projectAdapters';
import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { bandTokens } from '../../project/bandTokens';
import { KindBadge } from '../KindBadge';
import {
  noAttemptStateFor,
  PROVENANCE_LABEL,
  taskDetailStateFill,
} from '../detail/detailPaneState.ts';
import { centeredScrollFor } from '../lens/lensGeometry.ts';
import {
  GRADE_LABEL,
  ProvenanceGroupStamp,
  ProvenanceRailMark,
  readProvenance,
} from '../provenance';
import type { LensSelection } from '../lens/useLensSelection';
import type { ActivityNode, PhaseNode, TaskAttemptNode, TaskNode } from './activityTree.ts';
import {
  currentPhaseExpansionIds,
  matchingTaskIds,
  needsInlineProvenanceMark,
} from './activityScope.ts';
import { emptyListCopyFor } from './listEmptyState.ts';
import {
  applyOperatorExpansion,
  deepLinkKey,
  deepLinkReveal,
  linkAlreadyShown,
  rememberShownLink,
  NO_EXPANSION,
  openByOperator,
  revealForQuery,
  type TreeExpansion,
} from './searchExpansion.ts';
import {
  ACTIVITY_GRID_GAP_PX,
  activityGridColumns,
  activityRowState,
  attemptRowState,
  bookKeyFor,
  chipFor,
  criticalBorderPx,
  idColumnWidthCh,
  currentStageMarker,
  effortBarFraction,
  floatPresentation,
  inlineActionsFor,
  isCurrentStage,
  LIST_COMPACT_BELOW_PX,
  listSlotVars,
  progressPresentationFor,
  retryCounterLabel,
  ROW_STATE_LABEL,
  stageRule,
  stateSlotFor,
  taskRowState,
  TITLE_MIN_PX,
  type ProgressPresentation,
  type RowState,
} from './activityRowPresentation.ts';

// The pure rules are re-exported beside the component so a caller (and the
// tests, which cannot load a `.tsx` module at all) reaches them either way.
export {
  chipFor,
  emphasisRank,
  floatPresentation,
  inlineActionsFor,
  type RowState,
} from './activityRowPresentation.ts';

// ---------------------------------------------------------------------------
// Geometry constants — one place, so the three tiers cannot drift apart.
// ---------------------------------------------------------------------------

/** Indent per tier, in px. Applied from the MODEL's tier rather than from the
 *  library's depth CSS variable: the indent is part of this design, not a
 *  default we inherit and might silently lose to a version bump. */
const TIER_INDENT: Record<TreeTier, number> = { activity: 0, stage: 16, task: 34 };

/** The float rail's own width; its HEIGHT is constant — float is not a length. */
const FLOAT_RAIL_WIDTH = 3;
/** The provenance rail's column, reserved on every tier BEFORE the tier indent
 *  so the three tiers' rails stack into one uninterrupted vertical band. */
const PROVENANCE_RAIL_PX = 4;
const STAGE_WEIGHT_TRACK_PX = 48;

/** The tier-1 grid, shared by every activity row and the column header. */
const ACTIVITY_GRID_COLUMNS = activityGridColumns(PROVENANCE_RAIL_PX);

/**
 * The list's slot widths, as custom properties the grid reads, switched by a
 * CONTAINER query on the list's own width — not the viewport: the list is what
 * narrows when the detail pane opens. Narrow, the kind badge drops to its icon
 * and the float/effort/progress tracks shrink; the provenance and state slots,
 * and the spelled-out `≈ RECONSTRUCTED` badge, never do.
 */
const LIST_CONTAINER = 'constructionlist';
const LIST_SLOT_SX = {
  ...listSlotVars('wide'),
  '& [data-kind-icon]': { display: 'none' },
  [`@container ${LIST_CONTAINER} (max-width: ${String(LIST_COMPACT_BELOW_PX - 1)}px)`]: {
    ...listSlotVars('compact'),
    '& [data-kind-full]': { display: 'none' },
    '& [data-kind-icon]': { display: 'inline-flex' },
    // The compact float/effort slots are 30/36px, exactly the width of "FLOAT" and
    // "EFFORT" at 9px with wide tracking — so "FLOAT EFFORT ID" ran together at
    // 1600 with the pane open (designer re-check N5). Compact labels set smaller
    // and tighter, which leaves each one a visible gutter inside its own slot.
    [`& [data-testid="${UI_IDENTIFIERS.Construction.LIST_HEADER}"] .MuiTypography-root`]: {
      fontSize: 8,
      letterSpacing: '0.02em',
    },
  },
} as const;

/** A deep link centres once its row has held still this many frames — i.e. its
 *  ancestors' Collapse has finished growing (designer final N1) … */
const DEEP_LINK_STABLE_FRAMES = 4;
/** … or after this long, whichever comes first. */
const DEEP_LINK_SETTLE_CAP_MS = 1500;

/** The nearest scrolling ancestor — the console's scroller, found the way the
 *  lens shell finds it. */
function scrollParentOf(el: HTMLElement): HTMLElement | null {
  let node: HTMLElement | null = el.parentElement;
  while (node !== null && !/(auto|scroll)/.test(getComputedStyle(node).overflowY)) {
    node = node.parentElement;
  }
  return node;
}

// ---------------------------------------------------------------------------
// The item model
// ---------------------------------------------------------------------------

type TreeTier = 'activity' | 'stage' | 'task';

/**
 * One tree item. `children` is what RichTreeView traverses; every other field is
 * ours, read back inside the row with `useTreeItemModel`.
 *
 * `label` exists because the tree needs a searchable string per item (type-ahead
 * today, Task 11's search-reveal next) — it is never what the row renders.
 */
interface TreeRow {
  id: string;
  label: string;
  tier: TreeTier;
  activity: ActivityNode;
  stage?: PhaseNode;
  task?: TaskNode;
  children?: TreeRow[];
}

function itemsFor(nodes: readonly ActivityNode[]): TreeRow[] {
  return nodes.map((activity) => ({
    id: activity.nodeId,
    label: `${activity.activityId} ${activity.label}`,
    tier: 'activity' as const,
    activity,
    children: activity.phases.map((stage) => ({
      id: stage.nodeId,
      label: stage.name,
      tier: 'stage' as const,
      activity,
      stage,
      children: stage.tasks.map((task) => ({
        id: task.nodeId,
        label: task.label,
        tier: 'task' as const,
        activity,
        stage,
        task,
      })),
    })),
  }));
}

/** Every item, flattened by id — the selection handler gets an id, not a model. */
function indexOf(rows: readonly TreeRow[], into: Map<string, TreeRow>): Map<string, TreeRow> {
  for (const r of rows) {
    into.set(r.id, r);
    if (r.children !== undefined) indexOf(r.children, into);
  }
  return into;
}

/** The URL selection this item stands for — a click never selects more than
 *  what was clicked, so a shallower click clears what was below it. */
function selectionFor(row: TreeRow): LensSelection {
  switch (row.tier) {
    case 'activity':
      return { activityId: row.activity.activityId };
    case 'stage':
      return {
        activityId: row.activity.activityId,
        ...(row.stage !== undefined ? { lifecyclePhase: row.stage.phase } : {}),
      };
    case 'task':
      return {
        activityId: row.activity.activityId,
        ...(row.task !== undefined
          ? { lifecyclePhase: row.task.lifecyclePhase, task: row.task.task }
          : {}),
      };
  }
}

/** The item id the URL's selection points at — deepest-wins, and `null` (not
 *  `undefined`) when nothing is selected, which is how MUI reads "no selection"
 *  for a controlled single-select tree. */
function selectedItemId(selection: LensSelection): string | null {
  const { activityId, lifecyclePhase, task } = selection;
  if (activityId === undefined) return null;
  if (lifecyclePhase === undefined) return activityId;
  if (task === undefined) return `${activityId}::${lifecyclePhase}`;
  return `${activityId}::${lifecyclePhase}::${task}`;
}

// ---------------------------------------------------------------------------
// Row context — the item slot receives only `itemId`, so everything else the
// row needs travels here rather than through slotProps (which would spread
// unknown props onto a DOM <li>).
// ---------------------------------------------------------------------------

interface RowContextValue {
  t: Tokens;
  /** The widest effort on screen — the effort bar is a fraction of it. */
  maxEffortDays: number;
  /** The id column's width in `ch`, sized to the longest id on screen (P0-4).
   *  One value for every row: each row is its own grid, so a shared width is
   *  what keeps the column aligned down the list. */
  idColumnCh: number;
  onInlineRetry: (selection: LensSelection) => void;
  /** Task nodeIds a live search matched by their own key/label (Task 11).
   *  Empty when the search box is empty. */
  searchMatchedTaskIds: ReadonlySet<string>;
}

const RowContext = createContext<RowContextValue | undefined>(undefined);

function useRowContext(): RowContextValue {
  const value = useContext(RowContext);
  // Unreachable in the rendered tree (the provider wraps RichTreeView), but the
  // context is genuinely optional at the type level and a thrown error beats a
  // silently un-themed row.
  if (value === undefined) throw new Error('ActivityTreeView row rendered outside its provider');
  return value;
}

// ---------------------------------------------------------------------------
// The component
// ---------------------------------------------------------------------------

export interface ActivityTreeViewProps {
  /** Already filtered/sorted/searched — this file neither decides membership
   *  nor order (see ../list/activityScope.ts); it renders and reveals. */
  nodes: readonly ActivityNode[];
  selection: LensSelection;
  onSelect: (selection: LensSelection) => void;
  /** The toolbar's raw search text. A tier-3 match auto-expands its ancestors
   *  and is focused (and thus scrolled into view) via `apiRef`; empty means no
   *  search is active. */
  searchQuery: string;
  /** Bumped by the toolbar's "Expand to current phase" button. Every increase
   *  (additively) expands every activity currently in flight; `0` (the
   *  initial value, never reached again once incremented) fires nothing on
   *  mount. */
  expandToCurrentPhaseSignal: number;
  /** How many activities exist BEFORE the toolbar filtered them, so an empty
   *  list can tell "nothing matches" from "nothing exists" (listEmptyState.ts). */
  totalActivityCount: number;
  /** Resets every toolbar filter — offered only when the filters hid every row. */
  onClearFilters: () => void;
}

export function ActivityTreeView({
  nodes,
  selection,
  onSelect,
  searchQuery,
  expandToCurrentPhaseSignal,
  totalActivityCount,
  onClearFilters,
}: ActivityTreeViewProps): ReactElement {
  const t = useTokens();
  const apiRef = useRichTreeViewApiRef();
  // TWO lists, not one: every open row, and the subset a SEARCH opened that the
  // operator has not touched since (searchExpansion.ts). Clearing the query
  // closes only what the search opened (designer P1-10) — it used to leave all of
  // it open, 220 rows where the operator had left 29.
  const [expansion, setExpansion] = useState<TreeExpansion>(NO_EXPANSION);

  const items = useMemo(() => itemsFor(nodes), [nodes]);
  const byId = useMemo(() => indexOf(items, new Map<string, TreeRow>()), [items]);

  // The scale for the effort channel. Absent efforts contribute nothing — they
  // must not drag the scale to zero and they must not draw a bar (see
  // effortBarFraction), because a zero-length bar and "no estimate" would be the
  // same pixel.
  const maxEffortDays = useMemo(
    () => nodes.reduce((max, n) => Math.max(max, n.effortDays ?? 0), 0),
    [nodes]
  );

  // The id column's width, sized to the longest id actually on screen (P0-4):
  // a fixed 86px truncated 26 of 29 ids.
  const idColumnCh = useMemo(() => idColumnWidthCh(nodes.map((n) => n.activityId)), [nodes]);

  // Search matches are recomputed freely every render (cheap, purely a
  // rendering concern — which rows get the highlight + inline provenance
  // mark).
  const searchMatchedTaskIds = useMemo(() => {
    const ids = new Set<string>();
    for (const node of nodes) {
      for (const id of matchingTaskIds(node, searchQuery)) ids.add(id);
    }
    return ids;
  }, [nodes, searchQuery]);

  // Reveal, computed DURING RENDER rather than in an effect — the React-
  // sanctioned "adjust state when a prop changes" pattern this codebase
  // already uses for the toolbar store (useLensToolbar's `held`): comparing
  // against the LAST query this reveal ran for, and calling setState only
  // when it actually changed, so the loop terminates on the immediate re-
  // render exactly like every other instance of this pattern here. Reading
  // `nodes` directly (not through a ref) is what makes computing this safe
  // in the render body — it is simply this render's own prop, not a stale
  // capture — and it never fights a manual collapse the operator made
  // since this only runs again when `searchQuery` itself changes.
  const [reveal, setReveal] = useState<{ forQuery: string; focusTarget: string | null }>({
    forQuery: '',
    focusTarget: null,
  });
  if (reveal.forQuery !== searchQuery) {
    const toExpand = new Set<string>();
    let firstMatch: string | undefined;
    for (const node of nodes) {
      const ids = matchingTaskIds(node, searchQuery);
      if (ids.length === 0) continue;
      toExpand.add(node.nodeId);
      for (const phase of node.phases) {
        if (phase.tasks.some((task) => ids.includes(task.nodeId))) toExpand.add(phase.nodeId);
      }
      firstMatch ??= ids[0];
    }
    setReveal({ forQuery: searchQuery, focusTarget: firstMatch ?? null });
    // A new query REPLACES the previous query's reveal, and a cleared one closes
    // it — never a row the operator opened (searchExpansion.ts, designer P1-10).
    setExpansion((prev) => revealForQuery(prev, [...toExpand]));
  }

  // "Expand to current phase" — additive, and deliberately never "expand
  // all": only the activities actually in flight right now. Same render-time-
  // adjustment shape as the search reveal above, keyed on the toolbar's own
  // click signal rather than on `nodes`.
  const [appliedExpandSignal, setAppliedExpandSignal] = useState(0);
  if (appliedExpandSignal !== expandToCurrentPhaseSignal) {
    setAppliedExpandSignal(expandToCurrentPhaseSignal);
    if (expandToCurrentPhaseSignal !== 0) {
      const ids = currentPhaseExpansionIds(nodes);
      if (ids.length > 0) {
        // An explicit operator action: these rows are theirs, so clearing a
        // search never closes them.
        setExpansion((prev) => openByOperator(prev, ids));
      }
    }
  }

  // A DEEP LINK (?a=&p=&k=) opens its ancestors once, on the first render that
  // has rows, and names the selected row as the scroll target (designer re-check
  // N1) — before, a linked task sat hidden under a closed chevron beside a pane
  // describing it. Same render-time-adjustment shape as the reveals above; the
  // rows it opens are the operator's (a later search clear leaves them open).
  // `undefined` = not yet applied; `null` = applied, nothing to scroll to.
  //
  // Only for a selection no tree has shown yet (fix-C review N1): a lens switch
  // unmounts this tree, and re-running the reveal on the way back re-opened rows
  // the operator had since collapsed. searchExpansion.linkAlreadyShown remembers
  // the last selection shown, across remounts; it is written below, from an
  // effect, once this render has decided.
  const linkKey = deepLinkKey(selection);
  const [linkTarget, setLinkTarget] = useState<string | null | undefined>(undefined);
  if (linkTarget === undefined && nodes.length > 0) {
    const link = linkAlreadyShown(linkKey)
      ? { expand: [], target: null }
      : deepLinkReveal(selection);
    setLinkTarget(link.target);
    if (link.expand.length > 0) setExpansion((prev) => openByOperator(prev, link.expand));
  }
  useEffect(() => {
    if (linkTarget !== undefined) rememberShownLink(linkKey);
  }, [linkKey, linkTarget]);

  // Centre the linked row in the band below the sticky toolbar, once its
  // ancestors have finished expanding (designer final N1): wait until the row and
  // the scroll extent hold still for a few frames, then scroll. Where the list ends
  // too soon to centre it, the runway below the list makes the room
  // (lensGeometry.centeredScrollFor) — measured, the row used to land at 62-81% of
  // the band at 1280/1366 because the scroller was already at its maximum.
  // Imperative DOM only; no React state is set here.
  const runwayRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (linkTarget === null || linkTarget === undefined) return undefined;
    const target = linkTarget;
    const rowOf = (): HTMLElement | null =>
      document.querySelector<HTMLElement>(
        `[data-testid="${UI_IDENTIFIERS.Construction.listRow(target)}"]`
      );
    const center = (row: HTMLElement, scroller: HTMLElement): void => {
      const runway = runwayRef.current;
      const toolbar = scroller.querySelector<HTMLElement>(
        `[data-testid="${UI_IDENTIFIERS.Construction.LENS_TOOLBAR}"]`
      );
      const box = scroller.getBoundingClientRect();
      const rect = row.getBoundingClientRect();
      const plan = centeredScrollFor({
        rowTop: rect.top - box.top + scroller.scrollTop,
        rowHeight: rect.height,
        bandTop: toolbar?.offsetHeight ?? 0,
        bandBottom: Math.min(box.bottom, window.innerHeight) - box.top,
        contentHeight: scroller.scrollHeight - (runway?.offsetHeight ?? 0),
        clientHeight: scroller.clientHeight,
      });
      if (runway !== null) runway.style.height = `${String(plan.runwayPx)}px`;
      scroller.scrollTop = plan.scrollTop;
    };
    const started = performance.now();
    let last = '';
    let still = 0;
    let frame = 0;
    const tick = (): void => {
      const row = rowOf();
      const scroller = row === null ? null : scrollParentOf(row);
      if (row !== null && scroller === null) {
        row.scrollIntoView({ block: 'center' });
        return;
      }
      const where =
        row === null || scroller === null
          ? ''
          : `${String(Math.round(row.getBoundingClientRect().top + scroller.scrollTop))}:${String(scroller.scrollHeight)}`;
      still = where !== '' && where === last ? still + 1 : 0;
      last = where;
      const settled =
        still >= DEEP_LINK_STABLE_FRAMES || performance.now() - started > DEEP_LINK_SETTLE_CAP_MS;
      if (settled && row !== null && scroller !== null) {
        center(row, scroller);
        return;
      }
      if (performance.now() - started > DEEP_LINK_SETTLE_CAP_MS) return;
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return (): void => {
      cancelAnimationFrame(frame);
    };
  }, [linkTarget]);

  // The one genuine SIDE EFFECT here (an imperative DOM/library call, not a
  // state update): once a reveal names a focus target, `focusItem` also
  // scrolls it into view. It requires the target's ancestors to already be
  // expanded (TreeViewFocusPlugin's own doc comment says so) — which the
  // render-time adjustment above guarantees landed in the SAME commit this
  // effect runs after, whether or not this particular match needed a new
  // expansion (an already-open ancestor still takes this same path,
  // harmlessly).
  useEffect(() => {
    if (reveal.focusTarget === null) return undefined;
    const target = reveal.focusTarget;
    const frame = requestAnimationFrame((): void => {
      apiRef.current?.focusItem(null, target);
    });
    return (): void => {
      cancelAnimationFrame(frame);
    };
  }, [reveal.focusTarget, apiRef]);

  const onSelectedItemsChange = useCallback(
    (_event: SyntheticEvent | null, itemId: string | null): void => {
      if (itemId === null) return;
      const row = byId.get(itemId);
      if (row === undefined) return;
      onSelect(selectionFor(row));
    },
    [byId, onSelect]
  );

  const rowContext = useMemo(
    (): RowContextValue => ({
      t,
      maxEffortDays,
      idColumnCh,
      onInlineRetry: onSelect,
      searchMatchedTaskIds,
    }),
    [t, maxEffortDays, idColumnCh, onSelect, searchMatchedTaskIds]
  );

  // "Nothing matches" and "nothing exists" never share a sentence (P1-9): a search
  // that matched nothing used to say the whole project had no activities.
  const emptyCopy = emptyListCopyFor(nodes.length, totalActivityCount, searchQuery);

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.25 }}>
      {emptyCopy !== undefined ? (
        <Box
          data-testid={UI_IDENTIFIERS.Construction.LIST_EMPTY}
          sx={{
            border: `1.5px dashed ${t.line}`,
            borderRadius: `${String(t.radius)}px`,
            bgcolor: t.paperAlt,
            px: 3,
            py: 5,
            textAlign: 'center',
          }}
        >
          <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted }}>
            {emptyCopy.message}
          </Typography>
          {emptyCopy.offerClear ? (
            <Button
              data-testid={UI_IDENTIFIERS.Construction.LIST_CLEAR_FILTERS}
              size="small"
              sx={{
                mt: 1.5,
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: 11,
                letterSpacing: '0.04em',
                textTransform: 'none',
                color: t.ink,
                borderColor: t.line,
              }}
              variant="outlined"
              onClick={onClearFilters}
            >
              Clear filters
            </Button>
          ) : null}
        </Box>
      ) : (
        <RowContext.Provider value={rowContext}>
          <Box
            data-testid={UI_IDENTIFIERS.Construction.LIST_TREE}
            sx={{
              border: `1.5px solid ${t.line}`,
              borderRadius: `${String(t.radius)}px`,
              bgcolor: t.paper,
              overflow: 'hidden',
              containerType: 'inline-size',
              containerName: LIST_CONTAINER,
            }}
          >
            <Box sx={LIST_SLOT_SX}>
              <ActivityListHeader />
              <RichTreeView
                apiRef={apiRef}
                expandedItems={expansion.expanded}
                expansionTrigger="iconContainer"
                getItemId={(item: TreeRow) => item.id}
                getItemLabel={(item: TreeRow) => item.label}
                items={items}
                selectedItems={selectedItemId(selection)}
                slots={{ item: ActivityTreeItem }}
                sx={{ '& ul': { listStyle: 'none', m: 0, p: 0 } }}
                onExpandedItemsChange={(_e, ids) => {
                  // A hand-made expansion change: whatever the operator touched is
                  // theirs now, so a later search clear leaves it alone.
                  setExpansion((prev) => applyOperatorExpansion(prev, ids));
                }}
                onSelectedItemsChange={onSelectedItemsChange}
              />
            </Box>
          </Box>
        </RowContext.Provider>
      )}
      {/* The deep-link runway: blank room below the list, sized only when a linked
          row near the list's end needs it to reach the centre (see above). The
          negative margin cancels this column's gap, so at 0 it takes no space. */}
      <Box aria-hidden ref={runwayRef} sx={{ height: 0, mt: -1.25, flexShrink: 0 }} />
    </Box>
  );
}

// ---------------------------------------------------------------------------
// One tree item — MUI owns the semantics (role, tabindex, aria-expanded,
// keyboard traversal); this owns the pixels.
// ---------------------------------------------------------------------------

function ActivityTreeItem(props: TreeItemProps): ReactElement {
  const { id, itemId, label, disabled, children, ...forwarded } = props;
  // Spread conditionally rather than passing `undefined` through: the project
  // compiles with `exactOptionalPropertyTypes`, so an absent optional and one
  // holding `undefined` are genuinely different types here.
  const {
    getContextProviderProps,
    getRootProps,
    getContentProps,
    getIconContainerProps,
    getGroupTransitionProps,
    status,
  } = useTreeItem({
    itemId,
    label,
    children,
    ...(id !== undefined ? { id } : {}),
    ...(disabled !== undefined ? { disabled } : {}),
  });
  const model = useTreeItemModel<TreeRow>(itemId);
  const { t } = useRowContext();

  const rootProps = getRootProps<Record<string, unknown>>({ ...forwarded });

  return (
    <TreeItemProvider {...getContextProviderProps()}>
      {/* `outline: 0` matches MUI's own TreeItemRoot. The <li> wraps the whole
          SUBTREE, so a focus ring on it draws a box around every descendant row
          — seen on the first screenshot pass. The ring belongs on the row that
          actually has focus, which is the content below. */}
      <Box component="li" {...rootProps} sx={{ listStyle: 'none', m: 0, p: 0, outline: 0 }}>
        <Box
          {...getContentProps()}
          data-testid={UI_IDENTIFIERS.Construction.listRow(itemId)}
          sx={{
            display: 'flex',
            alignItems: 'stretch',
            cursor: 'pointer',
            '&:hover': { bgcolor: alpha(t.accent, 0.06) },
            '&[data-selected]': { bgcolor: alpha(t.accent, 0.12) },
            '&[data-focused]': { outline: `2px solid ${t.accent}`, outlineOffset: '-2px' },
          }}
        >
          {model === null ? null : (
            <RowBody
              expandable={status.expandable}
              expanded={status.expanded}
              iconContainerProps={getIconContainerProps()}
              row={model}
            />
          )}
        </Box>
        {/* The group MUST go through the transition slot, not a bare <ul>: its
            props carry `in` (the expanded flag) and `unmountOnExit`, which is
            what keeps a collapsed activity's ~14 descendant rows out of the DOM
            entirely. Rendering the children unconditionally put all 645 items on
            the screen at rest — measured, not assumed. */}
        {children !== undefined && children !== null ? (
          <Collapse {...getGroupTransitionProps()} sx={{ m: 0, p: 0 }} />
        ) : null}
      </Box>
    </TreeItemProvider>
  );
}

interface IconContainerProps {
  onClick: (event: ReactMouseEvent) => void;
}

function RowBody({
  row,
  expandable,
  expanded,
  iconContainerProps,
}: {
  row: TreeRow;
  expandable: boolean;
  expanded: boolean;
  iconContainerProps: IconContainerProps;
}): ReactElement | null {
  const chevron = <Chevron expandable={expandable} expanded={expanded} {...iconContainerProps} />;
  switch (row.tier) {
    case 'activity':
      return <ActivityRow chevron={chevron} node={row.activity} />;
    case 'stage':
      // A `stage`/`task` item without its node is unreachable (itemsFor builds
      // them together) but both are optional on the model, so narrow rather
      // than assert: a missing row renders nothing, never a crash.
      return row.stage === undefined ? null : (
        <StageRuleRow chevron={chevron} node={row.activity} stage={row.stage} />
      );
    case 'task':
      return row.task === undefined ? null : <TaskRow node={row.activity} task={row.task} />;
  }
}

function Chevron({
  expandable,
  expanded,
  onClick,
}: {
  expandable: boolean;
  expanded: boolean;
  onClick: (event: ReactMouseEvent) => void;
}): ReactElement {
  const { t } = useRowContext();
  return (
    <Box
      sx={{
        width: 18,
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: t.muted,
        cursor: expandable ? 'pointer' : 'default',
      }}
      onClick={(e) => {
        // Expansion and selection are DIFFERENT gestures. The icon container
        // sits inside the content, whose own onClick selects, so without this
        // the chevron would do both — and opening a row to look inside it would
        // silently move the operator's selection.
        onClick(e);
        e.stopPropagation();
      }}
    >
      {expandable ? (
        expanded ? (
          <ExpandMoreRoundedIcon sx={{ fontSize: 15 }} />
        ) : (
          <ChevronRightRoundedIcon sx={{ fontSize: 15 }} />
        )
      ) : null}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Tier 1 — the activity row. The only tier with a fill of its own.
// ---------------------------------------------------------------------------

function ActivityRow({
  chevron,
  node,
}: {
  chevron: ReactElement;
  node: ActivityNode;
}): ReactElement {
  const { t, maxEffortDays } = useRowContext();
  const state = activityRowState(node.row);
  const loud = state === 'awaitingHuman';
  // The contagion roll-up: worst provenance anywhere beneath this activity, so
  // a collapsed row cannot hide a reconstructed task.
  const provenance = useMemo(() => readProvenance(node), [node]);

  return (
    <Box
      sx={{
        flexGrow: 1,
        minWidth: 0,
        display: 'grid',
        // ONE template for every row and the header (activityGridColumns): fixed
        // kind / provenance / progress / state slots, so a no-record row's empty
        // slots hold their place and every column lines up (designer P1-8).
        gridTemplateColumns: ACTIVITY_GRID_COLUMNS,
        alignItems: 'center',
        columnGap: `${String(ACTIVITY_GRID_GAP_PX)}px`,
        pl: `${String(TIER_INDENT.activity)}px`,
        pr: 1.25,
        py: 0.6,
        // The criticality channel: a border WEIGHT, never a chip. 2px vs 3px is
        // too fine a difference on its own at this row height, so the critical
        // edge also takes the full line colour while every other row's recedes —
        // one channel expressed twice, which is not the double-encoding the rule
        // forbids (that is a geometry PLUS a chip). It goes amber only for the
        // state that is blocked on the reader.
        borderLeft: `${String(criticalBorderPx(node.onCriticalPath))}px solid ${
          loud ? t.accent : node.onCriticalPath === true ? t.line : alpha(t.line, 0.25)
        }`,
        borderBottom: `1px solid ${alpha(t.line, 0.22)}`,
        bgcolor: loud ? t.awaitingBg : 'transparent',
      }}
    >
      <ProvenanceRailMark reading={provenance} t={t} />
      {chevron}
      <FloatRail band={node.band} float={node.float} />
      <EffortBar days={node.effortDays} maxDays={maxEffortDays} />
      <IdTitleCell node={node} />
      <KindSlot node={node} />
      {/* The stamp sits immediately before the percentage it qualifies: on a
          backfilled row that numeral reads 100% ✓ PASSED from evidence
          reconstructed after the fact, never observed as it happened, and the
          badge must be read in the same glance as the claim. Its slot is fixed,
          so a row with no stamp keeps the column. */}
      <Box data-slot="provenance" sx={{ display: 'flex', alignItems: 'center', minWidth: 0 }}>
        <ProvenanceGroupStamp reading={provenance} t={t} />
      </Box>
      <ProgressFill presentation={progressPresentationFor(state, node.percentComplete)} />
      <StateSlotView retryCount={node.retryCount} state={state} />
    </Box>
  );
}

/**
 * The id and the title share ONE cell that wraps (fix-A concern 2). The id keeps
 * the column width sized to the longest id, so ids never truncate; the title keeps
 * at least TITLE_MIN_PX beside it, and when the row cannot give it that, the title
 * wraps UNDER the id at the full cell width instead of shrinking to nothing. Every
 * row shares both widths, so every row wraps or none does.
 */
function IdTitleCell({ node }: { node: ActivityNode }): ReactElement {
  const { t, idColumnCh } = useRowContext();
  const marker = currentStageMarker(node);
  return (
    <Box
      data-slot="idTitle"
      sx={{
        minWidth: 0,
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        columnGap: `${String(ACTIVITY_GRID_GAP_PX)}px`,
      }}
    >
      <Typography
        data-testid={UI_IDENTIFIERS.Construction.listIdCell(node.activityId)}
        sx={{
          // Sized to the longest id on screen (idColumnWidthCh, 12-32ch): a
          // fixed 86px cut 26 of 29 ids short (designer P0-4). The ellipsis only
          // ever applies past the 32ch clamp, and the title carries the full id.
          width: `${String(idColumnCh)}ch`,
          maxWidth: '100%',
          flexShrink: 0,
          fontFamily: t.mono,
          fontSize: 11.5,
          fontWeight: 700,
          color: t.ink,
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
        }}
        title={node.activityId}
      >
        {node.activityId}
      </Typography>
      <Box
        sx={{
          flex: `1 1 ${String(TITLE_MIN_PX)}px`,
          minWidth: 0,
          display: 'flex',
          alignItems: 'center',
          gap: 0.75,
        }}
      >
        <Typography
          data-testid={UI_IDENTIFIERS.Construction.listTitleCell(node.activityId)}
          sx={{
            minWidth: 0,
            fontFamily: t.body,
            fontSize: 12,
            color: node.label === node.activityId ? t.muted : t.ink,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
          title={node.label === node.activityId ? undefined : node.label}
        >
          {node.label === node.activityId ? '—' : node.label}
        </Typography>
        {marker !== undefined && !marker.inProfile ? <AbsentStage named={marker.named} /> : null}
      </Box>
    </Box>
  );
}

/** The kind slot: the badge, or — below the list's compact width — its icon
 *  alone (the kind is then the icon's accessible name and tooltip). */
function KindSlot({ node }: { node: ActivityNode }): ReactElement {
  const { t } = useRowContext();
  let full: ReactElement | null = null;
  let icon: ReactElement | null = null;
  if (node.unclassified) {
    const word = (
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9.5,
          letterSpacing: '0.06em',
          color: t.muted,
          opacity: 0.8,
        }}
      >
        UNCLASSIFIED
      </Typography>
    );
    full = word;
    icon = (
      <Tooltip title="Unclassified: the server could not derive a type for this activity">
        <Typography
          aria-label="Unclassified"
          sx={{ fontFamily: t.mono, fontSize: 11, color: t.muted }}
        >
          ?
        </Typography>
      </Tooltip>
    );
  } else if (node.kind !== undefined) {
    full = <KindBadge kind={node.kind} size="xs" t={t} />;
    icon = <KindBadge iconOnly kind={node.kind} size="xs" t={t} />;
  }
  return (
    <Box
      data-slot="kind"
      sx={{ display: 'flex', alignItems: 'center', minWidth: 0, overflow: 'hidden' }}
    >
      <Box data-kind-full="" sx={{ display: 'inline-flex', minWidth: 0 }}>
        {full}
      </Box>
      <Box data-kind-icon="">{icon}</Box>
    </Box>
  );
}

/** The state slot (stateSlotFor): a chip with its glyph; for NOT STARTED the
 *  hollow circle and muted words — never a chip; nothing for `unknown`. The `↻N`
 *  retry roll-up rides here too, the same numeral its task rows use. */
function StateSlotView({
  state,
  retryCount,
}: {
  state: RowState;
  retryCount: number;
}): ReactElement {
  const { t } = useRowContext();
  const slot = stateSlotFor(state);
  return (
    <Box
      data-slot="state"
      data-state-slot={slot.kind}
      sx={{ display: 'flex', alignItems: 'center', gap: 0.6, minWidth: 0, overflow: 'hidden' }}
    >
      {retryCount > 0 ? (
        <Tooltip title={`${String(retryCount)} retried task(s) in this activity`}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}>
            {`↻${String(retryCount)}`}
          </Typography>
        </Tooltip>
      ) : null}
      {slot.kind === 'chip' ? (
        <>
          <StateGlyph state={state} />
          <StateChip chip={slot.chip} />
        </>
      ) : null}
      {slot.kind === 'notStarted' ? (
        <>
          <StateGlyph state="notStarted" />
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 9.5,
              letterSpacing: '0.04em',
              color: t.muted,
              whiteSpace: 'nowrap',
            }}
          >
            {slot.label}
          </Typography>
        </>
      ) : null}
    </Box>
  );
}

/** The column header — the same grid as the rows beneath it, so each label sits
 *  over its column, including the id/title split (which wraps exactly as the rows
 *  do). */
function ActivityListHeader(): ReactElement {
  const { t, idColumnCh } = useRowContext();
  const cell = {
    fontFamily: t.mono,
    fontSize: 9,
    fontWeight: 700,
    letterSpacing: '0.08em',
    color: t.muted,
    textTransform: 'uppercase',
    whiteSpace: 'nowrap',
    overflow: 'hidden',
  } as const;
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.LIST_HEADER}
      sx={{
        display: 'grid',
        gridTemplateColumns: ACTIVITY_GRID_COLUMNS,
        alignItems: 'end',
        columnGap: `${String(ACTIVITY_GRID_GAP_PX)}px`,
        pr: 1.25,
        py: 0.5,
        borderLeft: '2px solid transparent',
        borderBottom: `1px solid ${alpha(t.line, 0.4)}`,
        bgcolor: t.paperAlt,
      }}
    >
      <Box />
      <Box />
      <Typography data-slot="float" sx={cell}>
        float
      </Typography>
      <Typography data-slot="effort" sx={cell}>
        effort
      </Typography>
      <Box
        sx={{
          minWidth: 0,
          display: 'flex',
          flexWrap: 'wrap',
          columnGap: `${String(ACTIVITY_GRID_GAP_PX)}px`,
        }}
      >
        <Typography
          sx={{
            ...cell,
            width: `${String(idColumnCh)}ch`,
            maxWidth: '100%',
            flexShrink: 0,
            fontSize: 9,
          }}
        >
          id
        </Typography>
        <Typography sx={{ ...cell, flex: `1 1 ${String(TITLE_MIN_PX)}px`, minWidth: 0 }}>
          title
        </Typography>
      </Box>
      <Tooltip title="Kind">
        {/* Tighter tracking: in the compact width this column is an icon's width. */}
        <Typography data-slot="kind" sx={{ ...cell, letterSpacing: '0.02em' }}>
          kind
        </Typography>
      </Tooltip>
      <Typography data-slot="provenance" sx={cell}>
        provenance
      </Typography>
      <Typography data-slot="progress" sx={cell}>
        progress
      </Typography>
      <Typography data-slot="state" sx={cell}>
        state
      </Typography>
    </Box>
  );
}

/** The four bands the server's policy emits. `ActivityMeta.band` is a plain
 *  string (passed through untouched from the network's compute block), so it is
 *  NARROWED here rather than asserted: an unrecognised band renders as no band
 *  at all, which is the honest reading of a value we cannot place. */
const FLOAT_BANDS: readonly FloatBand[] = ['critical', 'red', 'yellow', 'green'];

function asFloatBand(value: string | undefined): FloatBand | undefined {
  return FLOAT_BANDS.find((b) => b === value);
}

/** Float: a rail band (bandTokens) AND an always-visible numeral. Colour is
 *  never the sole carrier (WCAG 1.4.1), and an unknown float renders as a bare
 *  `—` — never as a green rail reading "plenty of slack", and never as a
 *  placeholder mark either: when most rows had no float on record, a hairline
 *  per row became a ruled-paper texture competing with the rows that DID have
 *  one (measured on the at-rest screenshot, not assumed). No data, no ink. */
function FloatRail({
  float,
  band,
}: {
  float: number | undefined;
  band: string | undefined;
}): ReactElement {
  const { t } = useRowContext();
  const known = float !== undefined;
  const bandName = asFloatBand(band);
  const presented = floatPresentation(float, bandName);
  const colour = bandName !== undefined ? bandTokens(t, bandName).fg : t.line;
  return (
    <Tooltip title={known ? `Total float: ${presented.numeral} days` : 'No float on record'}>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
        <Box
          sx={{
            width: FLOAT_RAIL_WIDTH,
            height: 14,
            flexShrink: 0,
            bgcolor: known ? colour : 'transparent',
          }}
        />
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10,
            fontWeight: known ? 700 : 400,
            color: known ? t.ink : t.muted,
          }}
        >
          {presented.numeral}
        </Typography>
      </Box>
    </Tooltip>
  );
}

/** Effort: a bar LENGTH and nothing else. No bar at all where there is no
 *  estimate — a row the committed activity list does not carry has none, and a
 *  zero-length bar would claim a zero-day activity. */
function EffortBar({ days, maxDays }: { days: number | undefined; maxDays: number }): ReactElement {
  const { t } = useRowContext();
  const fraction = effortBarFraction(days, maxDays);
  if (fraction === undefined) {
    // An empty cell, not a dashed placeholder: the track still reserves its
    // width so the columns stay aligned, but it draws nothing. Absence of a bar
    // IS the signal, the same way absence of a chip is.
    return <Box sx={{ width: '100%', height: 6 }} />;
  }
  return (
    <Tooltip title={`${String(days ?? 0)} days of effort`}>
      <Box
        sx={{
          width: '100%',
          height: 6,
          bgcolor: alpha(t.line, 0.12),
          borderRadius: 1,
          overflow: 'hidden',
        }}
      >
        <Box
          sx={{ width: `${String(fraction * 100)}%`, height: '100%', bgcolor: alpha(t.ink, 0.4) }}
        />
      </Box>
    </Tooltip>
  );
}

/** Progress: a fill EXTENT plus its numeral (progressPresentationFor). A
 *  NOT-STARTED activity draws an empty DASHED track and a muted "0%" — known work,
 *  none of it done. An unknowable percentage (no profile, or a phase unreported)
 *  draws no track and reads `—`: an unknown denominator is not a zero numerator. */
function ProgressFill({ presentation }: { presentation: ProgressPresentation }): ReactElement {
  const { t } = useRowContext();
  const tooltip =
    presentation.kind === 'fill'
      ? 'Σ Table A-1 weights of the complete phases'
      : presentation.kind === 'notStarted'
        ? 'Not started: no phase of this activity is complete'
        : 'Some phases are unreported';
  return (
    <Tooltip title={tooltip}>
      <Box
        data-progress-kind={presentation.kind}
        data-slot="progress"
        sx={{ display: 'flex', alignItems: 'center', gap: 0.5, minWidth: 0 }}
      >
        <Box
          sx={{
            width: 'var(--list-progress-track)',
            height: 6,
            flexShrink: 0,
            borderRadius: 1,
            boxSizing: 'border-box',
            bgcolor: presentation.kind === 'fill' ? alpha(t.line, 0.12) : 'transparent',
            border:
              presentation.kind === 'notStarted' ? `1px dashed ${alpha(t.line, 0.5)}` : 'none',
            overflow: 'hidden',
          }}
        >
          {presentation.kind === 'fill' ? (
            <Box
              sx={{
                width: `${String(presentation.percent)}%`,
                height: '100%',
                bgcolor: t.committedDot,
              }}
            />
          ) : null}
        </Box>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10,
            color: presentation.kind === 'fill' ? t.ink : t.muted,
            width: 32,
            flexShrink: 0,
            textAlign: 'right',
          }}
        >
          {presentation.label}
        </Typography>
      </Box>
    </Tooltip>
  );
}

/** The `absent` channel: a gap, 40% opacity, the name struck. A reported
 *  `CurrentPhase` the activity's profile has no node for (e.g. `integration`
 *  on a two-phase uiDesign profile) highlights nothing, and the row says
 *  exactly that. */
function AbsentStage({ named }: { named: string }): ReactElement {
  const { t } = useRowContext();
  return (
    <Tooltip
      title={`The server reports this activity is in "${named}", which its profile does not carry`}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9.5,
          color: t.muted,
          opacity: 0.4,
          textDecoration: 'line-through',
          whiteSpace: 'nowrap',
          flexShrink: 0,
        }}
      >
        {`▸ ${named}`}
      </Typography>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// Tier 2 — a group RULE, not a card.
// ---------------------------------------------------------------------------

function StageRuleRow({
  chevron,
  node,
  stage,
}: {
  chevron: ReactElement;
  node: ActivityNode;
  stage: PhaseNode;
}): ReactElement {
  const { t } = useRowContext();
  const heaviest = Math.max(...node.phases.map((p) => p.weight));
  const rule = stageRule(stage, heaviest);
  const current = isCurrentStage(node, stage);
  const nameColour = rule.filled ? t.committedText : rule.unreported ? t.muted : t.ink;
  const provenance = useMemo(() => readProvenance(stage), [stage]);

  return (
    <Box
      sx={{
        flexGrow: 1,
        minWidth: 0,
        display: 'flex',
        alignItems: 'stretch',
        bgcolor: alpha(t.line, 0.03),
      }}
    >
      <ProvenanceRailMark reading={provenance} t={t} />
      <Box
        sx={{
          flexGrow: 1,
          minWidth: 0,
          display: 'flex',
          alignItems: 'center',
          gap: 1,
          pl: `${String(TIER_INDENT.stage)}px`,
          pr: 1.25,
          py: 0.35,
        }}
      >
        {chevron}
        <Typography sx={{ fontFamily: t.mono, fontSize: 11, color: current ? t.accent : t.muted }}>
          ┌
        </Typography>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 10,
            fontWeight: 700,
            letterSpacing: '0.1em',
            color: nameColour,
            whiteSpace: 'nowrap',
          }}
        >
          {rule.name.toUpperCase()}
        </Typography>
        {current ? (
          <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, fontWeight: 700, color: t.accent }}>
            ▸ current
          </Typography>
        ) : null}
        {/* Tier 2 is the second and LAST tier that gets a badge. Its task rows
          inherit the rail alone — there are hundreds of them, and a chip on
          each is the density failure that got two prototype rounds rejected. */}
        <ProvenanceGroupStamp reading={provenance} t={t} />
        {/* The weight's magnitude channel: a segment WIDTH. Filled when the server
          reported the phase complete, hollow when it reported incomplete, dashed
          when it reported nothing — "no record" and "not done" are different. */}
        <Tooltip
          title={
            rule.unreported
              ? 'The server reported nothing about this phase'
              : rule.filled
                ? 'Exit criterion met'
                : 'Exit criterion not met'
          }
        >
          <Box
            sx={{
              width: STAGE_WEIGHT_TRACK_PX,
              height: 5,
              flexShrink: 0,
              border: rule.unreported
                ? `1px dashed ${alpha(t.line, 0.5)}`
                : `1px solid ${alpha(t.line, 0.45)}`,
              borderRadius: 1,
              overflow: 'hidden',
            }}
          >
            <Box
              sx={{
                width: `${String(rule.weightFraction * 100)}%`,
                height: '100%',
                bgcolor: rule.filled ? t.committedDot : 'transparent',
              }}
            />
          </Box>
        </Tooltip>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, flexShrink: 0 }}>
          {rule.weightLabel}
        </Typography>
        <Typography
          sx={{
            fontFamily: t.body,
            fontSize: 10.5,
            color: t.muted,
            minWidth: 0,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
        >
          {`· exit: ${rule.exitCriterion}`}
        </Typography>
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// Tier 3 — one Figure A-1 task. The majority of these are `unknown`, and the
// whole design rests on that reading as "here is the work, none of it has
// happened" rather than as a wall of errors.
// ---------------------------------------------------------------------------

function TaskRow({ node, task }: { node: ActivityNode; task: TaskNode }): ReactElement {
  const { t, onInlineRetry, searchMatchedTaskIds } = useRowContext();
  const [openAttempts, setOpenAttempts] = useState(false);
  const state = taskRowState(task, node.status, noAttemptStateFor(node.row));
  const chip = chipFor(state);
  const bookKey = bookKeyFor(task.label, task.bookLabel, task.task);
  const loud = state === 'awaitingHuman';
  const failed = state === 'failed';
  const counter = retryCounterLabel(task.attemptCount);
  // The task's OWN provenance — its attempt ledger and nothing else. A task row
  // gets the rail only: naming the sub-grade in ink on every row is what the
  // tooltip exists to replace.
  const provenance = useMemo(() => readProvenance(task), [task]);
  // A live search revealed THIS row specifically. The group header above it
  // still carries the `≈ RECONSTRUCTED` badge (the normal argument-from-
  // context), but a search reveal can scroll a task row into view on its own
  // — so a matched, reconstructed row ALSO carries its own inline mark
  // (below, via the pure `needsInlineProvenanceMark`), independent of whether
  // its ancestors are still on screen.
  const isSearchMatch = searchMatchedTaskIds.has(task.nodeId);
  const showsInlineProvenanceMark = needsInlineProvenanceMark(isSearchMatch, provenance.origin);

  return (
    <Box sx={{ flexGrow: 1, minWidth: 0 }}>
      <Box
        // The row's state as a hook for specs: the glyph drawn beside the label
        // (StateGlyph) is a pure function of it.
        data-task-state={state}
        sx={{
          display: 'flex',
          alignItems: 'stretch',
          borderLeft: isSearchMatch
            ? `3px solid ${t.accent}`
            : loud
              ? `3px solid ${t.accent}`
              : '3px solid transparent',
          bgcolor: isSearchMatch
            ? alpha(t.accent, 0.14)
            : loud || failed
              ? t.awaitingBg
              : 'transparent',
        }}
      >
        <ProvenanceRailMark reading={provenance} t={t} />
        <Box
          sx={{
            flexGrow: 1,
            minWidth: 0,
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            pl: `${String(TIER_INDENT.task)}px`,
            pr: 1.25,
            py: 0.3,
          }}
        >
          <StateGlyph state={state} />
          <Typography
            sx={{
              fontFamily: t.body,
              fontSize: 11.5,
              fontWeight: task.gate ? 700 : 400,
              color: failed
                ? t.dangerFg
                : state === 'unknown' || state === 'notStarted'
                  ? t.muted
                  : t.ink,
              opacity: state === 'skipped' ? 0.55 : 1,
              textDecoration: state === 'skipped' ? 'line-through' : 'none',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}
          >
            {task.label}
          </Typography>
          {task.gate ? (
            <Tooltip title="This task's success IS the phase's binary exit criterion (App A)">
              <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted, flexShrink: 0 }}>
                gate
              </Typography>
            </Tooltip>
          ) : null}
          {/* The book's own task KEY, trailing the row's words behind a separator
            ("Flow Review gate · stpReview"), only where this profile renamed the
            task and the key adds a word the label lacks (bookKeyFor). It used to
            sit between the label and the gate tag, where it read as part of the
            label (designer re-check N2). */}
          {bookKey !== undefined ? (
            <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.5, flexShrink: 0 }}>
              <Typography aria-hidden sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted }}>
                ·
              </Typography>
              <Tooltip title={`Figure A-1 task: ${task.bookLabel}`}>
                <Typography
                  data-testid={UI_IDENTIFIERS.Construction.listTaskBookKey(task.nodeId)}
                  sx={{
                    fontFamily: t.mono,
                    fontSize: 9,
                    color: t.muted,
                    opacity: 0.8,
                    whiteSpace: 'nowrap',
                  }}
                >
                  {bookKey}
                </Typography>
              </Tooltip>
            </Box>
          ) : null}
          {/* needsInlineProvenanceMark matches the GROUP badge's own rule
            (ProvenanceGroupStamp) exactly: only `reconstructed` ever earns a
            mark. `unknown` is already self-evidently quiet (chip-less, no
            rail) and asserts nothing a reader could mistake for fact, so it
            does not need one — this is the guarantee from Task 7's caveat,
            extended to a search reveal, and it is pinned by a test. */}
          {showsInlineProvenanceMark ? (
            <Tooltip title={<span style={{ whiteSpace: 'pre-line' }}>{provenance.tooltip}</span>}>
              <Typography
                data-testid={UI_IDENTIFIERS.Construction.searchMatchProvenance(task.nodeId)}
                sx={{
                  fontFamily: t.mono,
                  fontSize: 9,
                  fontWeight: 700,
                  letterSpacing: '0.04em',
                  color: t.muted,
                  flexShrink: 0,
                  whiteSpace: 'nowrap',
                }}
              >
                {`≈ ${GRADE_LABEL.reconstructed.toLowerCase()}`}
              </Typography>
            </Tooltip>
          ) : null}
          <Box sx={{ flexGrow: 1 }} />
          {/* The per-row origin WORD used to sit here. It is gone on purpose: the
            rail carries the grade and its tooltip carries the sub-grade plus the
            basis, so hundreds of rows no longer spell "backfilled" in ink beside work
            whose state chip already competes for the same glance. */}
          {counter !== undefined ? (
            <Box
              aria-expanded={openAttempts}
              aria-label={`${String(task.attemptCount)} attempts`}
              component="button"
              data-testid={UI_IDENTIFIERS.Construction.listAttempts(task.nodeId)}
              sx={{
                flexShrink: 0,
                fontFamily: t.mono,
                fontSize: 9.5,
                fontWeight: 700,
                color: t.ink,
                bgcolor: 'transparent',
                // The retry magnitude's second channel: a DOUBLED stroke.
                border: `2px solid ${alpha(t.line, 0.5)}`,
                borderRadius: 1,
                px: 0.4,
                py: 0,
                cursor: 'pointer',
              }}
              type="button"
              onClick={(e) => {
                // The row's own click selects the task; the counter only opens the
                // ledger, so reading the history never moves the selection.
                e.stopPropagation();
                setOpenAttempts((open) => !open);
              }}
            >
              {counter}
            </Box>
          ) : null}
          {inlineActionsFor(state).map((action) => (
            <Box
              component="button"
              data-testid={UI_IDENTIFIERS.Construction.listInlineAction(task.nodeId, action)}
              key={action}
              sx={{
                flexShrink: 0,
                fontFamily: t.mono,
                fontSize: 9.5,
                fontWeight: 700,
                color: t.dangerFg,
                bgcolor: 'transparent',
                border: `1px solid ${t.dangerFg}`,
                borderRadius: 1,
                px: 0.5,
                cursor: 'pointer',
              }}
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onInlineRetry({
                  activityId: task.activityId,
                  lifecyclePhase: task.lifecyclePhase,
                  task: task.task,
                });
              }}
            >
              ↻ Retry
            </Box>
          ))}
          <StateChip chip={chip} />
        </Box>
      </Box>
      {openAttempts ? <AttemptLedger task={task} /> : null}
    </Box>
  );
}

/** The attempt ledger, newest first — so a 3-retry task is still ONE row until
 *  someone asks, and the first thing they then read is the current attempt. */
function AttemptLedger({ task }: { task: TaskNode }): ReactElement {
  const { t } = useRowContext();
  const newestFirst = [...task.attempts].reverse();
  return (
    <Box sx={{ pl: `${String(TIER_INDENT.task + 22)}px`, pr: 1.25, pb: 0.5 }}>
      {newestFirst.map((attempt: TaskAttemptNode) => {
        const state = attemptRowState(attempt.superseded, outcomeState(attempt.outcome));
        return (
          <Box
            key={attempt.attemptId}
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1,
              py: 0.15,
              opacity: attempt.superseded ? 0.55 : 1,
            }}
          >
            <Typography
              sx={{ fontFamily: t.mono, fontSize: 9.5, fontWeight: 700, color: t.muted, width: 26 }}
            >
              {`↻${String(attempt.attempt)}`}
            </Typography>
            <StateGlyph state={state} />
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.ink }}>
              {ROW_STATE_LABEL[state]}
            </Typography>
            <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted }}>
              {`${PROVENANCE_LABEL[attempt.provenance.origin].toLowerCase()} · ${attempt.actor ?? '—'}`}
            </Typography>
          </Box>
        );
      })}
    </Box>
  );
}

/** One attempt's own outcome, read exactly as the tree reads it — with the
 *  `skipped` refinement this surface has a channel for. */
function outcomeState(outcome: TaskAttemptRow['outcome']): RowState {
  switch (outcome) {
    case 'passed':
      return 'passed';
    case 'skipped':
      return 'skipped';
    case 'rejected':
    case 'failed':
      return 'failed';
    case '':
      return 'running';
    default:
      return 'unknown';
  }
}

// ---------------------------------------------------------------------------
// The two shared marks
// ---------------------------------------------------------------------------

/** The state glyph — the geometry every state carries, chip or no chip. */
function StateGlyph({ state }: { state: RowState }): ReactElement {
  const { t } = useRowContext();
  const base = { width: 9, height: 9, flexShrink: 0 } as const;

  switch (state) {
    case 'unknown':
      // A hairline dashed outline, muted, NO fill. This is the majority mark on
      // the surface and it must read as "not yet", not as an alarm.
      return (
        <Tooltip title={ROW_STATE_LABEL.unknown}>
          <Box sx={{ ...base, border: `1px dashed ${alpha(t.line, 0.55)}` }} />
        </Tooltip>
      );
    case 'notStarted':
      return (
        <Tooltip title={ROW_STATE_LABEL.notStarted}>
          <Box sx={{ ...base, borderRadius: '50%', border: `1px solid ${t.muted}` }} />
        </Tooltip>
      );
    case 'running':
      // The ONLY animated element on the screen.
      return (
        <Tooltip title={ROW_STATE_LABEL.running}>
          <Box
            sx={{
              ...base,
              borderRadius: '50%',
              bgcolor: taskDetailStateFill(t, 'running').fg,
              '@keyframes constructionRunning': {
                '0%,100%': { opacity: 1 },
                '50%': { opacity: 0.25 },
              },
              animation: 'constructionRunning 1.4s ease-in-out infinite',
            }}
          />
        </Tooltip>
      );
    case 'awaitingHuman':
      return (
        <Tooltip title={ROW_STATE_LABEL.awaitingHuman}>
          <Box sx={{ ...base, borderRadius: '50%', bgcolor: t.awaitingFg }} />
        </Tooltip>
      );
    case 'passed':
      return (
        <Tooltip title={ROW_STATE_LABEL.passed}>
          <Typography
            sx={{ fontFamily: t.mono, fontSize: 11, lineHeight: 1, color: t.committedText }}
          >
            ✓
          </Typography>
        </Tooltip>
      );
    case 'failed':
      return (
        <Tooltip title={ROW_STATE_LABEL.failed}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 11, lineHeight: 1, color: t.dangerFg }}>
            ✕
          </Typography>
        </Tooltip>
      );
    case 'skipped':
      return (
        <Tooltip title={ROW_STATE_LABEL.skipped}>
          <Typography sx={{ fontFamily: t.mono, fontSize: 11, lineHeight: 1, color: t.muted }}>
            –
          </Typography>
        </Tooltip>
      );
    case 'superseded':
    case 'absent':
      return (
        <Tooltip title={ROW_STATE_LABEL[state]}>
          <Box sx={{ ...base, border: `1px solid ${alpha(t.line, 0.35)}`, opacity: 0.5 }} />
        </Tooltip>
      );
  }
}

/** The one chip a row may carry. Nothing renders when `chipFor` said nothing —
 *  which is the majority of rows, and the point. */
function StateChip({ chip }: { chip: ReturnType<typeof chipFor> }): ReactElement | null {
  const { t } = useRowContext();
  if (chip === undefined) return null;
  const fill = taskDetailStateFill(t, chip.state);
  return (
    <Box
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        flexShrink: 0,
        px: chip.size === 'xs' ? 0.55 : 0.75,
        py: chip.size === 'xs' ? 0.1 : 0.2,
        borderRadius: 99,
        border: `1px solid ${fill.border}`,
        bgcolor: fill.bg,
        color: fill.fg,
        fontFamily: t.mono,
        fontSize: chip.size === 'xs' ? 9 : 9.5,
        fontWeight: 700,
        letterSpacing: '0.06em',
        whiteSpace: 'nowrap',
      }}
    >
      {chip.label.toUpperCase()}
    </Box>
  );
}
