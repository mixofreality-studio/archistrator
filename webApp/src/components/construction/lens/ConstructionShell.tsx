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
 * lens, the toolbar state and the slots as props and reaches for no hooks beyond
 * useTokens. ExperienceChrome and the chat rail stay where they are, unchanged —
 * the shell renders INSIDE them.
 *
 * TASKS carries a count badge because it is the only lens that asserts something
 * is owed. GRAPH and TASKS render an honest "coming in a later stage" placeholder
 * until Stages C/D fill them; a placeholder that looked like data would be the
 * exact failure this rewrite exists to remove.
 */
import type { ReactElement, ReactNode } from 'react';
import Box from '@mui/material/Box';
import InputBase from '@mui/material/InputBase';
import MenuItem from '@mui/material/MenuItem';
import Select from '@mui/material/Select';
import ToggleButton from '@mui/material/ToggleButton';
import ToggleButtonGroup from '@mui/material/ToggleButtonGroup';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import SearchIcon from '@mui/icons-material/Search';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import type { Tokens } from '../../../utilities/theme/themes';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import {
  LENS_IDS,
  SCOPE_IDS,
  SORT_IDS,
  type LensId,
  type ScopeId,
  type SortId,
  type ToolbarState,
} from './useLensSelection';

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
  graph: 'The committed project network under a build lens',
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
  network: 'Network order',
  floatAsc: 'Float ascending',
};

/** Which scopes have a predicate wired this stage. The rest are announced, not faked. */
const LIVE_SCOPES: ReadonlySet<ScopeId> = new Set<ScopeId>(['all']);

export interface ConstructionShellProps {
  lens: LensId;
  /** How many tasks owe a human a decision — the TASKS badge. 0 renders no badge. */
  tasksOwed: number;
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
}

export function ConstructionShell({
  lens,
  tasksOwed,
  toolbar,
  kindOptions,
  layerOptions,
  header,
  content,
  detail,
  onLens,
  onToolbar,
}: ConstructionShellProps): ReactElement {
  const t = useTokens();

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', flexGrow: 1, minHeight: 0, minWidth: 0 }}>
      {header !== undefined ? header : null}

      <Box
        data-testid={UI_IDENTIFIERS.Construction.LENS_TOOLBAR}
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
        }}
      >
        <LensControl lens={lens} t={t} tasksOwed={tasksOwed} onLens={onLens} />

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
            placeholder="Search activity, title or component…"
            sx={{
              flexGrow: 1,
              fontFamily: t.mono,
              fontSize: 12.5,
              color: t.ink,
              '& input::placeholder': { color: t.muted, opacity: 1 },
            }}
            value={toolbar.search}
            onChange={(e) => {
              onToolbar({ search: e.target.value });
            }}
          />
        </Box>

        <ToolbarSelect
          label="Scope"
          options={SCOPE_IDS.map((id) => ({
            value: id,
            label: LIVE_SCOPES.has(id) ? SCOPE_LABEL[id] : `${SCOPE_LABEL[id]} (later stage)`,
            disabled: !LIVE_SCOPES.has(id),
          }))}
          t={t}
          testid={UI_IDENTIFIERS.Construction.LENS_SCOPE}
          value={toolbar.scope}
          onChange={(value) => {
            onToolbar({ scope: asScope(value) });
          }}
        />
        <ToolbarSelect
          label="Kind"
          options={[{ value: 'all', label: 'All kinds' }, ...kindOptions]}
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
        <ToolbarSelect
          // Tiers 2 and 3 are a Figure A-1 SEQUENCE — sorting them is nonsense
          // and is deliberately not on offer. Only tier 1 sorts.
          hint="Applies to activities only — phases and tasks keep their lifecycle order."
          label="Sort"
          options={SORT_IDS.map((id) => ({ value: id, label: SORT_LABEL[id] }))}
          t={t}
          testid={UI_IDENTIFIERS.Construction.LENS_SORT}
          value={toolbar.sort}
          onChange={(value) => {
            onToolbar({ sort: asSort(value) });
          }}
        />
      </Box>

      <Box sx={{ display: 'flex', flexGrow: 1, minHeight: 0, gap: 2 }}>
        <Box
          data-testid={UI_IDENTIFIERS.Construction.LENS_CONTENT}
          sx={{ flexGrow: 1, minWidth: 0 }}
        >
          {content}
        </Box>
        {detail !== undefined ? (
          <Box data-testid={UI_IDENTIFIERS.Construction.LENS_DETAIL} sx={{ flexShrink: 0 }}>
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
  tasksOwed,
  t,
  onLens,
}: {
  lens: LensId;
  tasksOwed: number;
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
          {id === 'tasks' && tasksOwed > 0 ? (
            <Box
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
            >
              {tasksOwed}
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
  label: string;
  disabled?: boolean;
}

function ToolbarSelect({
  label,
  options,
  value,
  t,
  testid,
  hint,
  onChange,
}: {
  label: string;
  options: readonly ToolbarOption[];
  value: string;
  t: Tokens;
  testid: string;
  hint?: string;
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
// The honest placeholder for a lens that has not been built yet
// ---------------------------------------------------------------------------

export function LensComingLater({ lens, stage }: { lens: LensId; stage: string }): ReactElement {
  const t = useTokens();
  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.LENS_PLACEHOLDER}
      sx={{
        border: `1.5px dashed ${t.line}`,
        borderRadius: `${String(t.radius)}px`,
        bgcolor: t.paperAlt,
        px: 3,
        py: 6,
        textAlign: 'center',
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 13,
          fontWeight: 700,
          letterSpacing: '0.06em',
          color: t.ink,
        }}
      >
        {LENS_LABEL[lens]} · Coming in a later stage
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 12, color: t.muted, mt: 1 }}>
        {LENS_HINT[lens]} — built in {stage}. Nothing is rendered here rather than sample rows, so
        this surface never shows anything the system cannot back.
      </Typography>
    </Box>
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
