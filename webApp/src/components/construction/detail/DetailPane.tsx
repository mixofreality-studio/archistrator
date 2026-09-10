/* eslint-disable react-refresh/only-export-components -- detailActionsFor (+ its two types) is
   re-exported alongside the component per the brief, the same colocation status.tsx/KindBadge.tsx
   already use for their token-driven helpers. */
/**
 * The shared detail pane — ONE header/body/action-bar surface behind every
 * lens (Stage B Task 4). Replaces ActivityLifecyclePanel's 480px overlay
 * Drawer, which covered the very thing you clicked from and, while open, its
 * modal backdrop also occluded the lens toolbar's right end.
 *
 * >= 1200px: laid out BESIDE the content — default 520px, resizable by
 * dragging the left edge, collapsible to a thin rail. Width is remembered in
 * localStorage (best-effort: every read and write is wrapped in try/catch —
 * it throws outright in a private window or with site data blocked, and the
 * pane must still render correctly with no stored value).
 *
 * < 1200px: degrades to the existing overlay Drawer (kept, not deleted —
 * ActivityLifecyclePanel.tsx still carries the original implementation for
 * reference; this is the SAME Drawer mechanism, now driven by the shared
 * header/body/action-bar rather than its own bespoke one).
 *
 * The two invariants that make this surface trustworthy (see
 * detailPaneState.ts for the pure half of both):
 *
 *   - The HEADER never changes shape across bodies: breadcrumb, state chip,
 *     provenance chip, attempt selector, exit criterion + Table A-1 weight.
 *     A reader must never have to work out which body layout they are
 *     looking at to find out what they have selected.
 *   - The ACTION BAR never changes shape either, and `↻ Run this task` is
 *     present and ENABLED in every state — including `passed` (re-run it)
 *     and `unknown` (run it for the first time). Failure is never terminal,
 *     made structural rather than conditional: see detailActionsFor.
 *
 * The body slot renders a placeholder for this task; Tasks 9–11 fill it with
 * the unknown / episode / review / artifact bodies.
 */
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactElement,
} from 'react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Drawer from '@mui/material/Drawer';
import IconButton from '@mui/material/IconButton';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import useMediaQuery from '@mui/material/useMediaQuery';
import CheckRoundedIcon from '@mui/icons-material/CheckRounded';
import ChevronLeftRoundedIcon from '@mui/icons-material/ChevronLeftRounded';
import ChevronRightRoundedIcon from '@mui/icons-material/ChevronRightRounded';
import CloseIcon from '@mui/icons-material/Close';

import type { ConstructionRow } from '../../../contracts/types';
import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { useLensSelection, type LensSelection } from '../lens/useLensSelection';
import {
  attemptProvenance,
  attemptsForTask,
  breadcrumbFor,
  detailActionsFor,
  PROVENANCE_LABEL,
  resolvePhaseTask,
  taskDetailStateFill,
  TASK_DETAIL_STATE_LABEL,
  taskDetailStateFor,
  WIDE_PANE_SX,
  type DetailAction,
  type TaskDetailState,
} from './detailPaneState.ts';

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
const WIDTH_STORAGE_KEY = 'archistrator.construction.detailPaneWidth';

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
  /** Human-readable activity title, falling back to the raw id when absent. */
  activityTitle?: string | undefined;
  onClose: () => void;
}

export function DetailPane({
  selection,
  row,
  activityTitle,
  onClose,
}: DetailPaneProps): ReactElement | null {
  const t = useTokens();
  const { select } = useLensSelection();
  const isWide = useMediaQuery(WIDE_BREAKPOINT);

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

  const state = useMemo(() => taskDetailStateFor(row, selection), [row, selection]);
  const actions = useMemo(() => detailActionsFor(state), [state]);
  const meta = useMemo(() => resolvePhaseTask(row, selection), [row, selection]);
  const origin = useMemo(() => attemptProvenance(row, selection), [row, selection]);
  const taskAttempts = useMemo(
    () =>
      selection.task !== undefined ? attemptsForTask(row?.attempts ?? [], selection.task) : [],
    [row, selection.task]
  );
  const effectiveAttempt = selection.attempt ?? taskAttempts[taskAttempts.length - 1]?.attempt;

  const label = activityTitle ?? activityId ?? '—';
  const breadcrumb = breadcrumbFor(label, meta.phaseName, meta.taskLabel, effectiveAttempt);

  if (!open) return null;

  const onSelectAttempt = (attempt: number): void => {
    select({ ...selection, attempt });
  };

  const body = <PlaceholderBody t={t} />;

  const paneContent = (
    <DetailPaneChrome
      actions={actions}
      body={body}
      breadcrumb={breadcrumb}
      collapsed={collapsed}
      exitCriterion={meta.exitCriterion}
      origin={origin}
      state={state}
      t={t}
      taskAttempts={taskAttempts}
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
      </Box>
    );
  }

  // Below 1200px: the existing overlay Drawer (kept, not deleted) — the pane
  // cannot sit beside content that no longer has room for it.
  return (
    <Drawer
      anchor="right"
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_DRAWER}
      open={open}
      slotProps={{
        paper: {
          'aria-labelledby': 'construction-detail-pane-title',
          role: 'dialog',
          sx: { width: { xs: '100%', sm: 480 }, bgcolor: t.paper, backgroundImage: 'none' },
        },
      }}
      onClose={onClose}
    >
      <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        <DetailHeader
          breadcrumb={breadcrumb}
          exitCriterion={meta.exitCriterion}
          origin={origin}
          state={state}
          t={t}
          taskAttempts={taskAttempts}
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
        <ActionBar actions={actions} t={t} />
      </Box>
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
  origin,
  taskAttempts,
  exitCriterion,
  weight,
  body,
  actions,
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
  origin: ReturnType<typeof attemptProvenance>;
  taskAttempts: ReturnType<typeof attemptsForTask>;
  exitCriterion: string | undefined;
  weight: number | undefined;
  body: ReactElement;
  actions: DetailAction[];
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
        <Tooltip placement="left" title="Expand detail pane">
          <IconButton
            aria-label="expand detail pane"
            data-testid={UI_IDENTIFIERS.Construction.DETAIL_COLLAPSE_TOGGLE}
            size="small"
            sx={{ color: t.ink }}
            onClick={onToggleCollapsed}
          >
            <ChevronLeftRoundedIcon fontSize="small" />
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
          exitCriterion={exitCriterion}
          origin={origin}
          state={state}
          t={t}
          taskAttempts={taskAttempts}
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
        <ActionBar actions={actions} t={t} />
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
  origin,
  taskAttempts,
  exitCriterion,
  weight,
  t,
  onClose,
  onCollapse,
  onSelectAttempt,
}: {
  breadcrumb: string;
  state: TaskDetailState;
  origin: ReturnType<typeof attemptProvenance>;
  taskAttempts: ReturnType<typeof attemptsForTask>;
  exitCriterion: string | undefined;
  weight: number | undefined;
  t: Tokens;
  onClose: () => void;
  onCollapse?: () => void;
  onSelectAttempt: (attempt: number) => void;
}): ReactElement {
  const fill = taskDetailStateFill(t, state);

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
          id="construction-detail-pane-title"
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
          <Tooltip placement="bottom" title="Collapse detail pane">
            <IconButton
              aria-label="collapse detail pane"
              data-testid={UI_IDENTIFIERS.Construction.DETAIL_COLLAPSE_TOGGLE}
              size="small"
              sx={{ color: t.muted }}
              onClick={onCollapse}
            >
              <ChevronRightRoundedIcon fontSize="small" />
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
          {TASK_DETAIL_STATE_LABEL[state].toUpperCase()}
        </Box>

        <Box
          data-testid={UI_IDENTIFIERS.Construction.DETAIL_PROVENANCE_CHIP}
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
          {origin !== undefined ? PROVENANCE_LABEL[origin].toUpperCase() : 'PROVENANCE UNKNOWN'}
        </Box>

        <AttemptSelector attempts={taskAttempts} t={t} onSelectAttempt={onSelectAttempt} />
      </Box>

      <Box
        data-testid={UI_IDENTIFIERS.Construction.DETAIL_EXIT_CRITERION}
        sx={{ display: 'flex', alignItems: 'baseline', gap: 0.75, mt: 1 }}
      >
        <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.4 }}>
          {exitCriterion !== undefined ? `Exit: ${exitCriterion}` : 'Exit: —'}
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
    </Box>
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

function ActionBar({ actions, t }: { actions: DetailAction[]; t: Tokens }): ReactElement {
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.DETAIL_ACTION_BAR}
      sx={{
        flexShrink: 0,
        display: 'flex',
        gap: 1,
        px: 2,
        py: 1.25,
        borderTop: `1.5px solid ${t.line}`,
        bgcolor: t.paperAlt,
      }}
    >
      {actions.map((a) => (
        <Button
          data-testid={UI_IDENTIFIERS.Construction.detailAction(a.id)}
          disabled={a.disabled}
          key={a.id}
          size="small"
          startIcon={a.id === 'approve' ? <CheckRoundedIcon /> : undefined}
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11.5,
            textTransform: 'none',
            ...(a.id === 'run'
              ? { color: t.bg, bgcolor: t.accent, '&:hover': { bgcolor: t.accent2 } }
              : a.id === 'approve'
                ? { color: t.committedFg, borderColor: t.committedDot }
                : { color: t.muted, borderColor: t.line }),
          }}
          variant={a.id === 'run' ? 'contained' : 'outlined'}
        >
          {a.label}
        </Button>
      ))}
    </Box>
  );
}

// ---------------------------------------------------------------------------
// The body slot — a placeholder for this task. Tasks 9–11 replace this with
// the unknown / episode / review / artifact bodies dispatched on `state` and
// the selection's task kind.
// ---------------------------------------------------------------------------

function PlaceholderBody({ t }: { t: Tokens }): ReactElement {
  return (
    <Box
      sx={{
        border: `1.5px dashed ${t.line}`,
        borderRadius: `${String(t.radius)}px`,
        bgcolor: t.paperAlt,
        px: 2,
        py: 4,
        textAlign: 'center',
      }}
    >
      <Typography sx={{ fontFamily: t.mono, fontSize: 11.5, color: t.muted, lineHeight: 1.6 }}>
        Body renders in a later stage-B task. The header and action bar above are the real,
        invariant surface — this box is the only placeholder here.
      </Typography>
    </Box>
  );
}
