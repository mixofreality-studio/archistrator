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
 * unknown is the majority state (292 of this project's 384 task rows). A screen
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
 */
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactElement,
  type SyntheticEvent,
} from 'react';
import Box from '@mui/material/Box';
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
import { PROVENANCE_LABEL, taskDetailStateFill } from '../detail/detailPaneState.ts';
import { ProvenanceGroupStamp, ProvenanceRailMark, readProvenance } from '../provenance';
import type { LensSelection } from '../lens/useLensSelection';
import type { ActivityNode, PhaseNode, TaskAttemptNode, TaskNode } from './activityTree.ts';
import {
  activityRowState,
  attemptRowState,
  chipFor,
  criticalBorderPx,
  currentStageMarker,
  effortBarFraction,
  floatPresentation,
  inlineActionsFor,
  isCurrentStage,
  percentLabel,
  retryCounterLabel,
  ROW_STATE_LABEL,
  stageRule,
  taskRowState,
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
const EFFORT_TRACK_PX = 54;
const PROGRESS_TRACK_PX = 56;
const STAGE_WEIGHT_TRACK_PX = 48;

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
  onInlineRetry: (selection: LensSelection) => void;
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
  /** Already derived and already ordered — this file neither sorts nor filters. */
  nodes: readonly ActivityNode[];
  selection: LensSelection;
  onSelect: (selection: LensSelection) => void;
}

export function ActivityTreeView({
  nodes,
  selection,
  onSelect,
}: ActivityTreeViewProps): ReactElement {
  const t = useTokens();
  const apiRef = useRichTreeViewApiRef();
  const [expandedItems, setExpandedItems] = useState<string[]>([]);

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

  const onSelectedItemsChange = useCallback(
    (_event: SyntheticEvent | null, itemId: string | null): void => {
      if (itemId === null) return;
      const row = byId.get(itemId);
      if (row !== undefined) onSelect(selectionFor(row));
    },
    [byId, onSelect]
  );

  const rowContext = useMemo(
    (): RowContextValue => ({ t, maxEffortDays, onInlineRetry: onSelect }),
    [t, maxEffortDays, onSelect]
  );

  if (nodes.length === 0) {
    return (
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
          No construction activities are recorded for this project yet.
        </Typography>
      </Box>
    );
  }

  return (
    <RowContext.Provider value={rowContext}>
      <Box
        data-testid={UI_IDENTIFIERS.Construction.LIST_TREE}
        sx={{
          border: `1.5px solid ${t.line}`,
          borderRadius: `${String(t.radius)}px`,
          bgcolor: t.paper,
          overflow: 'hidden',
        }}
      >
        <RichTreeView
          apiRef={apiRef}
          expandedItems={expandedItems}
          expansionTrigger="iconContainer"
          getItemId={(item: TreeRow) => item.id}
          getItemLabel={(item: TreeRow) => item.label}
          items={items}
          selectedItems={selectedItemId(selection)}
          slots={{ item: ActivityTreeItem }}
          sx={{ '& ul': { listStyle: 'none', m: 0, p: 0 } }}
          onExpandedItemsChange={(_e, ids) => {
            setExpandedItems(ids);
          }}
          onSelectedItemsChange={onSelectedItemsChange}
        />
      </Box>
    </RowContext.Provider>
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
      // both together) but the fields are genuinely optional on the model, so
      // narrow rather than assert: a missing row renders nothing, never a crash.
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
  const chip = chipFor(state);
  const loud = state === 'awaitingHuman';
  const marker = currentStageMarker(node);
  // The contagion roll-up: worst provenance anywhere beneath this activity, so
  // a collapsed row cannot hide a reconstructed task.
  const provenance = useMemo(() => readProvenance(node), [node]);

  return (
    <Box
      sx={{
        flexGrow: 1,
        minWidth: 0,
        display: 'grid',
        gridTemplateColumns: `${String(PROVENANCE_RAIL_PX)}px 18px 44px ${String(EFFORT_TRACK_PX)}px 86px minmax(0, 1fr) auto`,
        alignItems: 'center',
        gap: 1,
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
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 11.5,
          fontWeight: 700,
          color: t.ink,
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
        }}
      >
        {node.activityId}
      </Typography>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, minWidth: 0 }}>
        <Typography
          sx={{
            fontFamily: t.body,
            fontSize: 12,
            color: node.label === node.activityId ? t.muted : t.ink,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
        >
          {node.label === node.activityId ? '—' : node.label}
        </Typography>
        {marker !== undefined && !marker.inProfile ? <AbsentStage named={marker.named} /> : null}
      </Box>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexShrink: 0 }}>
        {node.unclassified ? (
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
        ) : node.kind !== undefined ? (
          <KindBadge kind={node.kind} size="xs" t={t} />
        ) : null}
        {/* The stamp sits immediately before the percentage it qualifies: on 21
            of the 69 rows that numeral reads 100% ✓ PASSED off a founder ruling
            with no artifact behind it, and the badge must be read in the same
            glance as the claim, not somewhere else on the row. */}
        <ProvenanceGroupStamp reading={provenance} t={t} />
        <ProgressFill percent={node.percentComplete} />
        {node.retryCount > 0 ? (
          // The activity's roll-up of the SAME channel its task rows use: a
          // numeral, never an extra row per retry.
          <Tooltip title={`${String(node.retryCount)} retried task(s) in this activity`}>
            <Typography sx={{ fontFamily: t.mono, fontSize: 10, fontWeight: 700, color: t.muted }}>
              {`↻${String(node.retryCount)}`}
            </Typography>
          </Tooltip>
        ) : null}
        {/* The state's GEOMETRY sits beside its chip — and only where there is
            a chip. A glyph on all 43 chip-less rows would put a mark back on
            every "we have no record" row, which is exactly what chip-less is
            supposed to say. The animated running dot lives here. */}
        {chip !== undefined ? <StateGlyph state={state} /> : null}
        <StateChip chip={chip} />
      </Box>
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
 *  placeholder mark either: with 60 of 69 rows unjoined, a hairline per row
 *  became a ruled-paper texture competing with the 9 rows that DO have a float
 *  (measured on the at-rest screenshot, not assumed). No data, no ink. */
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
 *  estimate — 60 of the 69 committed rows do not join the derived activity list
 *  (the 40-vs-69 seam), and a zero-length bar would claim a zero-day activity. */
function EffortBar({ days, maxDays }: { days: number | undefined; maxDays: number }): ReactElement {
  const { t } = useRowContext();
  const fraction = effortBarFraction(days, maxDays);
  if (fraction === undefined) {
    // An empty cell, not a dashed placeholder: the track still reserves its
    // width so the columns stay aligned, but it draws nothing. Absence of a bar
    // IS the signal, the same way absence of a chip is.
    return <Box sx={{ width: EFFORT_TRACK_PX, height: 6 }} />;
  }
  return (
    <Tooltip title={`${String(days ?? 0)} days of effort`}>
      <Box
        sx={{
          width: EFFORT_TRACK_PX,
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

/** Progress: a fill EXTENT plus its numeral. An unknowable percentage (any
 *  profile phase unreported — 43 of 69 rows) draws no track at all and reads
 *  `—`, never a 0% bar: an unknown denominator is not a zero numerator. */
function ProgressFill({ percent }: { percent: number | undefined }): ReactElement {
  const { t } = useRowContext();
  const known = percent !== undefined;
  return (
    <Tooltip
      title={known ? 'Σ Table A-1 weights of the complete phases' : 'Some phases are unreported'}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
        <Box
          sx={{
            width: PROGRESS_TRACK_PX,
            height: 6,
            borderRadius: 1,
            bgcolor: known ? alpha(t.line, 0.12) : 'transparent',
            overflow: 'hidden',
          }}
        >
          {known ? (
            <Box
              sx={{
                width: `${String(percent)}%`,
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
            color: known ? t.ink : t.muted,
            width: 32,
            textAlign: 'right',
          }}
        >
          {percentLabel(percent)}
        </Typography>
      </Box>
    </Tooltip>
  );
}

/** The `absent` channel: a gap, 40% opacity, the name struck. G-SPA's
 *  `CurrentPhase` is `integration` and its two-phase uiDesign profile has no
 *  such node — so nothing is highlighted, and the row says exactly that. */
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
          inherit the rail alone — 384 of them, and 384 chips is the density
          failure that got two prototype rounds rejected. */}
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
// happened" rather than as 292 errors.
// ---------------------------------------------------------------------------

function TaskRow({ node, task }: { node: ActivityNode; task: TaskNode }): ReactElement {
  const { t, onInlineRetry } = useRowContext();
  const [openAttempts, setOpenAttempts] = useState(false);
  const state = taskRowState(task, node.status);
  const chip = chipFor(state);
  const loud = state === 'awaitingHuman';
  const failed = state === 'failed';
  const counter = retryCounterLabel(task.attemptCount);
  // The task's OWN provenance — its attempt ledger and nothing else. A task row
  // gets the rail only: naming the sub-grade in ink on every row is what the
  // tooltip exists to replace.
  const provenance = useMemo(() => readProvenance(task), [task]);

  return (
    <Box sx={{ flexGrow: 1, minWidth: 0 }}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'stretch',
          borderLeft: loud ? `3px solid ${t.accent}` : '3px solid transparent',
          bgcolor: loud || failed ? t.awaitingBg : 'transparent',
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
              color: failed ? t.dangerFg : state === 'unknown' ? t.muted : t.ink,
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
          <Box sx={{ flexGrow: 1 }} />
          {/* The per-row origin WORD used to sit here. It is gone on purpose: the
            rail carries the grade and its tooltip carries the sub-grade plus the
            basis, so 218 rows no longer spell "backfilled" in ink beside work
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
