/**
 * ConstructionShell — the construction console's ONE surface: a shared toolbar,
 * a lens control, a content slot and a persistent detail slot.
 *
 * Why a lens control and not tabs
 * -------------------------------
 * Tabs unmount the detail pane and drop selection on every switch. This console
 * polls the project read every 1.5s while the construction pump cascades, which
 * already forced NetworkView to keep a module-level selection store keyed by a
 * content signature — the poll's remount was wiping the operator's selection
 * mid-glance. Tabs would multiply that across three surfaces. A lens control
 * inside ONE mounted tree keeps one set of state:
 *
 *   - SELECTION lives in the URL (useLensSelection) — nothing in the tree owns it;
 *   - TOOLBAR state lives in a signature-keyed module store (useLensToolbar) — it
 *     describes the DATASET, not the lens, so it survives both a lens switch and
 *     a remount.
 *
 * This component is presentation only (the pure `components` layer): it takes the
 * lens, the toolbar state and the slots as props and reaches for no app hooks
 * beyond useTokens. ExperienceChrome and the chat rail stay where they are,
 * unchanged — the shell renders INSIDE them.
 *
 * GEOMETRY (fix round A, designer P0-1/P0-2): the shell MEASURES its toolbar and
 * its scroller and publishes both as CSS custom properties (see lensGeometry.ts),
 * which is how the detail pane pins below the toolbar at whatever height it
 * wrapped to. It also flags the toolbar `data-stuck` once stuck, for its shadow.
 * Both are written to the DOM from a layout effect, never through React state —
 * the properties onto the DETAIL SLOT's wrapper, not this root (they inherit, and
 * a write on the root restyled the whole tree on every scroll; fix-A review I4),
 * and only when a value actually changed.
 *
 * TASKS carries a count badge because it is the only lens that asserts something
 * is owed. GRAPH and TASKS render an honest "coming in a later stage" placeholder
 * until Stages C/D fill them; a placeholder that looked like data would be the
 * exact failure this rewrite exists to remove.
 *
 * NAVIGABILITY (Stage B Task 11)
 * ------------------------------
 * A resting tree of ~30 tier-1 rows (hundreds once expanded) gets no free
 * sort/filter/virtualize — `@mui/x-tree-view` is the community edition, not a
 * DataGrid. Every scope chip is wired here (the 7 that shipped disabled in
 * Task 3 now have a real predicate behind them, in ../list/activityScope.ts),
 * and the one new imperative action — "Expand to current phase" — is
 * deliberately NOT "expand all": it opens only the 1-3 activities actually in
 * flight right now. "Expand all" on a 528-row corpus is the exact trap this
 * whole design exists to avoid.
 */
import { useLayoutEffect, useRef, type ReactElement, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import { alpha } from '@mui/material/styles';
import Button from '@mui/material/Button';
import InputBase from '@mui/material/InputBase';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import Switch from '@mui/material/Switch';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import SearchIcon from '@mui/icons-material/Search';
import UnfoldMoreRoundedIcon from '@mui/icons-material/UnfoldMoreRounded';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { KindBadge, KIND_META, type ActivityKind } from '../KindBadge';
import {
  LENS_IDS,
  SCOPE_IDS,
  SORT_IDS,
  type LensId,
  type ScopeId,
  type SortId,
  type ToolbarState,
} from './useLensSelection';
import { isToolbarStuck, lensGeometryVars, varsToWrite } from './lensGeometry';
import { LIST_LENS_ONLY, RANKED_LABEL, toolbarForLens } from './toolbarForLens';
import { tasksBadgeFor, type TasksBadge } from '../tasks/tasksLensCopy';

// ---------------------------------------------------------------------------
// Vocabulary
// ---------------------------------------------------------------------------

const LENS_LABEL: Record<LensId, string> = {
  list: '▤ LIST',
  graph: '⬡ GRAPH',
  tasks: '⚑ TASKS',
};

const LENS_HINT: Record<LensId, string> = {
  list: 'Every activity, its lifecycle phases and its tasks',
  graph: 'The architecture, layer by layer — each component carrying its lifecycle',
  tasks: 'Only the tasks that owe someone a decision',
};

const SCOPE_LABEL: Record<ScopeId, string> = {
  all: 'All',
  critical: 'Critical path',
  near: 'Near-critical',
  awaitingMe: 'Awaiting me',
  inFlight: 'In flight',
  hasRetries: 'Has retries',
  reconstructed: 'Reconstructed only',
  unknown: 'Unknown only',
};

const SORT_LABEL: Record<SortId, string> = {
  network: 'Network order (default)',
  floatAsc: 'Float ascending',
};

/** The sort menu's own helper text — WHY only two options exist, stated where
 *  the operator is looking rather than left for them to notice by absence.
 *  Tiers 2/3 are a Figure A-1 SEQUENCE (phase = Method order, task = execution
 *  order); sorting a sequence is nonsense, so it is explained, not offered. */
const SORT_HELP_TEXT =
  'Sort applies to activities only. Phase and task order is a Figure A-1 sequence — Method order for phases, execution order for tasks — and sorting a sequence would be nonsense, so it is not offered.';

/** Every scope chip now carries a real predicate (see ../list/activityScope.ts)
 *  — Task 11 wires the 7 that Task 3 shipped disabled. */
const ACTIVITY_KIND_KEYS: ReadonlySet<string> = new Set<string>(Object.keys(KIND_META));

/** The search hint. Its length sizes the field's minimum width, so it is never cut. */
const SEARCH_PLACEHOLDER = 'Search id, title or component…';

function asActivityKind(value: string): ActivityKind | undefined {
  return ACTIVITY_KIND_KEYS.has(value) ? (value as ActivityKind) : undefined;
}

export interface ConstructionShellProps {
  lens: LensId;
  /** How many tasks owe a human a decision — the TASKS badge. 0 renders no badge. */
  tasksOwed: number;
  /** In-flight activities whose session probe has not answered: the badge reads
   *  "?" (or "N?") rather than a count that may be short (tasksBadgeFor). */
  tasksUnchecked?: number;
  toolbar: ToolbarState;
  /** Live facet values from the dataset; an empty list renders just "All kinds". */
  kindOptions: readonly { value: string; label: string }[];
  layerOptions: readonly { value: string; label: string }[];
  /** Rendered above the toolbar (title, subtitle, primary action). */
  header?: ReactNode;
  content: ReactNode;
  /** The persistent detail slot — one mounted pane across every lens. */
  detail?: ReactNode;
  onLens: (lens: LensId) => void;
  onToolbar: (patch: Partial<ToolbarState>) => void;
  /** "Expand to current phase" — an IMPERATIVE action, not toolbar state, so it
   *  lives outside ToolbarState: clicking it a second time must re-open
   *  whatever the operator has since collapsed, which a persisted flag cannot
   *  express. The tree (ActivityTreeView) owns what "current phase" resolves
   *  to; this button only asks it to act. */
  onExpandToCurrentPhase: () => void;
  /** Whether anything is in flight to expand to, and the tooltip that says so —
   *  activityScope.expandToCurrentPhaseControl. */
  expandToCurrentPhase: { enabled: boolean; tooltip: string };
  /** When set, Sort is disabled with this reason — a lens whose tier-1 order is
   *  not the operator's to choose (the graph's positions are the architecture's). */
  sortDisabledReason?: string;
}

export function ConstructionShell({
  lens,
  tasksOwed,
  tasksUnchecked = 0,
  toolbar,
  kindOptions,
  layerOptions,
  header,
  content,
  detail,
  onLens,
  onToolbar,
  onExpandToCurrentPhase,
  expandToCurrentPhase,
  sortDisabledReason,
}: ConstructionShellProps): ReactElement {
  const t = useTokens();
  const controls = toolbarForLens(lens);
  const rootRef = useRef<HTMLDivElement>(null);
  const toolbarRef = useRef<HTMLDivElement>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const detailRef = useRef<HTMLDivElement>(null);
  const hasDetail = detail !== undefined;

  // MEASURE the toolbar, the scroller and the content row, and publish them as
  // CSS custom properties on the DETAIL SLOT's wrapper (lensGeometry.ts) — the
  // detail pane pins itself below the toolbar at whatever height it wrapped to,
  // and caps its height at the room it really has, at rest and pinned alike. The
  // same pass flags the toolbar `data-stuck` once it has stuck, for its shadow.
  // Written straight to the DOM, never through React state: a resize or a scroll
  // re-renders nothing. A layout effect, so the first values land before paint.
  //
  // On the wrapper, not the root (fix-A review I4): custom properties inherit, so
  // a write on the root invalidated the style of every element in the list below
  // it on every scroll event. Re-run when the pane mounts or unmounts, since that
  // is when the wrapper appears; a value that did not change is not written.
  useLayoutEffect(() => {
    const root = rootRef.current;
    const toolbar = toolbarRef.current;
    const row = rowRef.current;
    if (root === null || toolbar === null || row === null) return undefined;
    let scroller: HTMLElement | null = root.parentElement;
    while (scroller !== null && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) {
      scroller = scroller.parentElement;
    }
    const written: Record<string, string> = {};
    let writtenStuck: string | undefined;
    const publish = (): void => {
      const scrollerTop = scroller?.getBoundingClientRect().top ?? 0;
      const toolbarRect = toolbar.getBoundingClientRect();
      const stuck = String(
        isToolbarStuck({
          toolbarTop: toolbarRect.top,
          scrollerTop,
          scrollTop: scroller?.scrollTop ?? window.scrollY,
        })
      );
      if (stuck !== writtenStuck) {
        toolbar.setAttribute('data-stuck', stuck);
        writtenStuck = stuck;
      }
      const target = hasDetail ? detailRef.current : null;
      if (target === null) return;
      const vars = lensGeometryVars(
        toolbarRect.height,
        scroller?.clientHeight ?? window.innerHeight,
        row.getBoundingClientRect().top - scrollerTop,
        scroller !== null ? Number.parseFloat(getComputedStyle(scroller).paddingBottom) || 0 : 0
      );
      for (const [name, value] of varsToWrite(written, vars)) {
        target.style.setProperty(name, value);
        written[name] = value;
      }
    };
    publish();
    // The root is observed too: when the header above the toolbar changes height
    // (its subtitle wraps), the row's offset moves with it.
    const observer = new ResizeObserver(publish);
    observer.observe(root);
    observer.observe(toolbar);
    if (scroller !== null) observer.observe(scroller);
    // Scroll moves the row's offset (and the stuck state). Scroll events already
    // arrive at most once per frame, so this publishes synchronously.
    const scrollTarget: HTMLElement | Window = scroller ?? window;
    scrollTarget.addEventListener('scroll', publish, { passive: true });
    return (): void => {
      observer.disconnect();
      scrollTarget.removeEventListener('scroll', publish);
    };
  }, [hasDetail]);

  return (
    <Box
      ref={rootRef}
      sx={{ display: 'flex', flexDirection: 'column', flexGrow: 1, minHeight: 0, minWidth: 0 }}
    >
      {header !== undefined ? header : null}

      <Box
        data-stuck="false"
        data-testid={UI_IDENTIFIERS.Construction.LENS_TOOLBAR}
        ref={toolbarRef}
        sx={{
          position: 'sticky',
          top: 0,
          zIndex: 3,
          flexShrink: 0,
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 1,
          px: 1.25,
          py: 1,
          mb: 2,
          bgcolor: t.paperAlt,
          border: `1.5px solid ${t.line}`,
          borderRadius: `${String(t.radius)}px`,
          transition: 'box-shadow 120ms ease-out',
          // A light shadow ONLY while stuck: at rest the toolbar sits in the page
          // and a shadow would read as a floating panel; once rows scroll beneath
          // it, the shadow is what says they are passing under, not ending.
          '&[data-stuck="true"]': { boxShadow: `0 3px 8px ${alpha(t.ink, 0.14)}` },
        }}
      >
        <LensControl
          badge={tasksBadgeFor(tasksOwed, tasksUnchecked)}
          lens={lens}
          t={t}
          onLens={onLens}
        />

        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 0.75,
            flexGrow: 1,
            minWidth: 180,
            px: 1,
            py: 0.25,
            border: `1px solid ${t.line}`,
            borderRadius: `${String(t.radius)}px`,
            bgcolor: t.paper,
          }}
        >
          <SearchIcon sx={{ fontSize: 16, color: t.muted }} />
          <InputBase
            data-testid={UI_IDENTIFIERS.Construction.LENS_SEARCH}
            inputProps={{ 'aria-label': 'Search activities' }}
            placeholder={SEARCH_PLACEHOLDER}
            sx={{
              flexGrow: 1,
              fontFamily: t.mono,
              fontSize: 12.5,
              color: t.ink,
              '& input::placeholder': { color: t.muted, opacity: 1 },
              // The field never narrows below its own placeholder, so the hint is
              // never clipped mid-word (designer re-check N5: "…or componer" at
              // 1600). In a monospace face one `ch` is one glyph, so this is exact;
              // the toolbar wraps before the placeholder is cut.
              '& input': { minWidth: `${String(SEARCH_PLACEHOLDER.length)}ch` },
            }}
            value={toolbar.search}
            onChange={(e) => {
              onToolbar({ search: e.target.value });
            }}
          />
        </Box>

        <ToolbarSelect
          label="Scope"
          options={SCOPE_IDS.map((id) => ({ value: id, label: SCOPE_LABEL[id] }))}
          t={t}
          testid={UI_IDENTIFIERS.Construction.LENS_SCOPE}
          value={toolbar.scope}
          onChange={(value) => {
            onToolbar({ scope: asScope(value) });
          }}
        />
        <ToolbarSelect
          label="Kind"
          options={[
            { value: 'all', label: 'All kinds' },
            ...kindOptions.map((o) => {
              const kind = asActivityKind(o.value);
              return {
                value: o.value,
                // Reuses KindBadge (the same chip the tree rows themselves
                // render) rather than a second, plain-text kind vocabulary.
                label: kind !== undefined ? <KindBadge kind={kind} size="xs" t={t} /> : o.label,
              };
            }),
          ]}
          t={t}
          testid={UI_IDENTIFIERS.Construction.LENS_KIND}
          value={toolbar.kind}
          onChange={(value) => {
            onToolbar({ kind: value });
          }}
        />
        <ToolbarSelect
          label="Layer"
          options={[{ value: 'all', label: 'All layers' }, ...layerOptions]}
          t={t}
          testid={UI_IDENTIFIERS.Construction.LENS_LAYER}
          value={toolbar.layer}
          onChange={(value) => {
            onToolbar({ layer: value });
          }}
        />
        {controls.sort === 'menu' ? (
          <ToolbarSelect
            // Tiers 2 and 3 are a Figure A-1 SEQUENCE — sorting them is nonsense
            // and is deliberately not on offer. Only tier 1 sorts. Stated twice:
            // once as the hover hint on the control, once as a leading disabled
            // row INSIDE the opened menu — the brief asks for it in the sort
            // menu's own helper text, not only on hover.
            disabled={sortDisabledReason !== undefined}
            helperItem={SORT_HELP_TEXT}
            hint={sortDisabledReason ?? SORT_HELP_TEXT}
            label="Sort"
            options={SORT_IDS.map((id) => ({ value: id, label: SORT_LABEL[id] }))}
            t={t}
            testid={UI_IDENTIFIERS.Construction.LENS_SORT}
            value={toolbar.sort}
            onChange={(value) => {
              onToolbar({ sort: asSort(value) });
            }}
          />
        ) : (
          // The TASKS lens has its own order (owedRanking.ts); a Sort menu it
          // ignores would be a control that does nothing (designer P1-1).
          <Typography
            data-testid={UI_IDENTIFIERS.Construction.LENS_SORT_RANKED}
            sx={{
              flexShrink: 0,
              fontFamily: t.mono,
              fontSize: 10.5,
              fontWeight: 700,
              letterSpacing: '0.06em',
              color: t.muted,
              whiteSpace: 'nowrap',
            }}
          >
            {RANKED_LABEL}
          </Typography>
        )}

        {/* ONE no-wrap group (designer final items): "Expand to current phase" and
            "Observed only" wrap TOGETHER onto the toolbar's second row, never one
            without the other — at 1600 "Observed only" used to wrap alone. */}
        <Box
          data-testid={UI_IDENTIFIERS.Construction.LENS_TOOLBAR_TOGGLES}
          sx={{ display: 'flex', alignItems: 'center', flexWrap: 'nowrap', gap: 1, flexShrink: 0 }}
        >
          <Tooltip title={controls.listControls ? expandToCurrentPhase.tooltip : LIST_LENS_ONLY}>
            {/* A disabled button fires no pointer events, so the tooltip hangs off
                this wrapper: the operator still learns WHY there is nothing to open. */}
            <Box component="span" sx={{ display: 'inline-flex', flexShrink: 0 }}>
              <Button
                data-testid={UI_IDENTIFIERS.Construction.LENS_EXPAND_TO_PHASE}
                disabled={!controls.listControls || !expandToCurrentPhase.enabled}
                size="small"
                startIcon={<UnfoldMoreRoundedIcon sx={{ fontSize: 15 }} />}
                sx={{
                  flexShrink: 0,
                  fontFamily: t.mono,
                  fontWeight: 700,
                  fontSize: 11,
                  letterSpacing: '0.04em',
                  textTransform: 'none',
                  color: t.ink,
                  borderColor: t.line,
                  // Visibly OFF (designer re-check N5): muted ink alone read as a
                  // live button at a glance. Half opacity and a dashed border are the
                  // surface's own "not available" marks.
                  '&.Mui-disabled': {
                    color: t.muted,
                    borderColor: alpha(t.line, 0.5),
                    borderStyle: 'dashed',
                    opacity: 0.5,
                  },
                }}
                variant="outlined"
                onClick={onExpandToCurrentPhase}
              >
                Expand to current phase
              </Button>
            </Box>
          </Tooltip>

          <Tooltip
            title={
              controls.listControls
                ? 'Count only what the running system observed. Every activity stays listed; evidence reconstructed after the fact (backfilled or synthesized) is set aside, so an activity known only from it reads as not started.'
                : LIST_LENS_ONLY
            }
          >
            {/* Visibly OFF where it does not apply (designer re-check): the same
                half-opacity "not available" mark as Expand above — MUI's disabled
                switch alone still read as a live toggle. */}
            <Box
              data-testid={UI_IDENTIFIERS.Construction.LENS_OBSERVED_ONLY_TOGGLE}
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 0.4,
                flexShrink: 0,
                opacity: controls.listControls ? 1 : 0.5,
                cursor: controls.listControls ? 'default' : 'not-allowed',
              }}
            >
              <Switch
                checked={toolbar.observedOnly}
                data-testid={UI_IDENTIFIERS.Construction.LENS_OBSERVED_ONLY}
                disabled={!controls.listControls}
                size="small"
                slotProps={{ input: { 'aria-label': 'Observed only' } }}
                onChange={(e) => {
                  onToolbar({ observedOnly: e.target.checked });
                }}
              />
              <Typography
                sx={{
                  fontFamily: t.mono,
                  fontSize: 10.5,
                  fontWeight: 700,
                  letterSpacing: '0.06em',
                  color: t.muted,
                  textTransform: 'uppercase',
                  whiteSpace: 'nowrap',
                }}
              >
                Observed only
              </Typography>
            </Box>
          </Tooltip>
        </Box>
      </Box>

      {/* The content row: where the detail pane sits in flow at rest. Its
          offset from the scroller's top is one of the three measurements the
          pane's height cap reads (--lens-row-top). */}
      <Box ref={rowRef} sx={{ display: 'flex', flexGrow: 1, minHeight: 0, gap: 2 }}>
        <Box
          data-testid={UI_IDENTIFIERS.Construction.LENS_CONTENT}
          sx={{ flexGrow: 1, minWidth: 0 }}
        >
          {content}
        </Box>
        {detail !== undefined ? (
          <Box
            data-testid={UI_IDENTIFIERS.Construction.LENS_DETAIL}
            ref={detailRef}
            sx={{ flexShrink: 0 }}
          >
            {detail}
          </Box>
        ) : null}
      </Box>
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The ▤ LIST │ ⬡ GRAPH │ ⚑ TASKS segmented control
// ---------------------------------------------------------------------------

function LensControl({
  lens,
  badge,
  t,
  onLens,
}: {
  lens: LensId;
  /** The TASKS badge (tasksBadgeFor); absent renders none. */
  badge: TasksBadge | undefined;
  t: Tokens;
  onLens: (lens: LensId) => void;
}): ReactElement {
  return (
    <ToggleButtonGroup
      // `exclusive` — a lens is a single choice. Without it MUI runs the group in
      // multi-select mode and hands the handler a combined value, which the
      // codec would (correctly) reject as an unknown lens.
      exclusive
      aria-label="Construction lens"
      size="small"
      sx={{
        flexShrink: 0,
        '& .MuiToggleButton-root': {
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11.5,
          letterSpacing: '0.06em',
          textTransform: 'none',
          color: t.muted,
          borderColor: t.line,
          px: 1.25,
          py: 0.5,
          gap: 0.5,
          '&:hover': { color: t.ink, bgcolor: t.paper },
          '&.Mui-selected': {
            color: t.accentText,
            bgcolor: t.accent,
            '&:hover': { bgcolor: t.accent2 },
          },
        },
      }}
      value={lens}
      onChange={(_e, next: LensId | null) => {
        // null == clicking the already-selected button; a lens is never "none".
        if (next !== null) onLens(next);
      }}
    >
      {LENS_IDS.map((id) => (
        <ToggleButton
          data-testid={UI_IDENTIFIERS.Construction.lensButton(id)}
          key={id}
          title={LENS_HINT[id]}
          value={id}
        >
          {LENS_LABEL[id]}
          {id === 'tasks' && badge !== undefined ? (
            <Box
              aria-label={badge.title}
              component="span"
              data-testid={UI_IDENTIFIERS.Construction.LENS_TASKS_COUNT}
              sx={{
                ml: 0.5,
                px: 0.6,
                py: 0.05,
                borderRadius: `${String(t.radius)}px`,
                bgcolor: lens === 'tasks' ? t.accentText : t.awaitingBg,
                color: lens === 'tasks' ? t.accent : t.awaitingFg,
                fontSize: 10.5,
                fontWeight: 800,
              }}
              title={badge.title}
            >
              {badge.text}
            </Box>
          ) : null}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  );
}

// ---------------------------------------------------------------------------
// One toolbar dropdown. Dropdowns (not chips) because these are dynamic-count
// selections — the house convention for anything that can exceed ~5 options.
// ---------------------------------------------------------------------------

interface ToolbarOption {
  value: string;
  /** Usually plain text; the Kind select embeds a KindBadge here instead of a
   *  second, plain-text kind vocabulary. */
  label: ReactNode;
  disabled?: boolean;
}

/** A value no real option ever carries, so the helper row can never be the
 *  Select's controlled `value` and can never fire `onChange` (it is also
 *  `disabled`, which already blocks a click — this is the defence for the
 *  keyboard-typeahead path MUI's Select still runs over disabled items). */
const HELPER_ITEM_VALUE = '__helper__';

function ToolbarSelect({
  label,
  options,
  value,
  t,
  testid,
  hint,
  helperItem,
  disabled,
  onChange,
}: {
  label: string;
  options: readonly ToolbarOption[];
  value: string;
  t: Tokens;
  testid: string;
  hint?: string;
  /** Rendered as a disabled, non-selectable leading row INSIDE the opened
   *  menu — the sort control's "why only two options" belongs where the
   *  operator is looking (the menu itself), not only in a hover tooltip. */
  helperItem?: string;
  /** Greyed out; `hint` then says why. */
  disabled?: boolean;
  onChange: (value: string) => void;
}): ReactElement {
  const control = (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5, flexShrink: 0 }}>
      <Typography
        component="span"
        sx={{
          fontFamily: t.mono,
          fontSize: 10.5,
          fontWeight: 700,
          letterSpacing: '0.08em',
          color: t.muted,
          textTransform: 'uppercase',
        }}
      >
        {label}
      </Typography>
      <Select
        data-testid={testid}
        disabled={disabled === true}
        inputProps={{ 'aria-label': label }}
        size="small"
        sx={{
          fontFamily: t.mono,
          fontSize: 12,
          color: t.ink,
          bgcolor: t.paper,
          '& .MuiOutlinedInput-notchedOutline': { borderColor: t.line },
          '& .MuiSelect-select': { py: 0.5, pl: 1 },
          '& .MuiSvgIcon-root': { color: t.muted },
        }}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      >
        {helperItem !== undefined ? (
          <MenuItem
            disabled
            divider
            sx={{
              fontFamily: t.body,
              fontSize: 10.5,
              color: t.muted,
              whiteSpace: 'normal',
              opacity: 1,
            }}
            value={HELPER_ITEM_VALUE}
          >
            {helperItem}
          </MenuItem>
        ) : null}
        {options.map((o) => (
          <MenuItem
            disabled={o.disabled}
            key={o.value}
            sx={{ fontFamily: t.mono, fontSize: 12 }}
            value={o.value}
          >
            {o.label}
          </MenuItem>
        ))}
      </Select>
    </Box>
  );
  return hint !== undefined ? (
    <Tooltip title={hint}>
      <Box sx={{ display: 'flex' }}>{control}</Box>
    </Tooltip>
  ) : (
    control
  );
}

// ---------------------------------------------------------------------------
// Narrowing helpers — a Select hands back a plain string.
// ---------------------------------------------------------------------------

function asScope(value: string): ScopeId {
  const found = SCOPE_IDS.find((id) => id === value);
  return found ?? 'all';
}

function asSort(value: string): SortId {
  const found = SORT_IDS.find((id) => id === value);
  return found ?? 'network';
}
