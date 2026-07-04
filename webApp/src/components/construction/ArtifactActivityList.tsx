/**
 * The left-column activity list for the Artifacts tab. Displays every activity
 * that has a construction row (joined with the activity-list name), with kind
 * badge, status chip, progress bar, and artifact count. Ported from the ux-mock
 * ArtifactsTab.tsx ListRow + list container, bound to real ConstructionRow data.
 *
 * This rail is the Artifacts tab's PRIMARY navigation, so it is a real ARIA
 * listbox: a roving-tabindex selectable list (↑/↓ move focus, Home/End jump,
 * Enter/Space select the focused activity and load its detail). Each row also
 * carries a discoverable "Comment on this activity" button — revealed on row
 * hover/focus, or armed from the keyboard with `c` — that arms an
 * activityConstruction anchor in the CommentContext so the operator can attach a
 * comment that rides the next phase-gate send-back.
 *
 * We hand-roll the listbox rather than reuse CommentableList because this rail's
 * PRIMARY action is selection (Enter/Space loads the activity detail), whereas
 * CommentableList binds Enter to comment-arming — using it here would hijack the
 * tab's primary nav. The comment affordance (button + `c` shortcut) is layered on
 * top with the same visual idiom.
 */
import { useCallback, useRef, useState, type ReactNode } from 'react';
import Box from '@mui/material/Box';
import Paper from '@mui/material/Paper';
import Typography from '@mui/material/Typography';
import IconButton from '@mui/material/IconButton';
import Tooltip from '@mui/material/Tooltip';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import type { Tokens } from '../../theme/themes';
import type { ConstructionRow } from '../../api/types';
import { StatusChip } from './status';
import type { BuildStatus } from '../../api/constructionAdapters';
import { KindBadge, kindColor } from './KindBadge';
import { useComments, activityConstructionAnchor } from '../comments/CommentContext';
import { UI_IDENTIFIERS } from '../../constants/UIIdentifiers';

/** A view-model row joining a ConstructionRow with the activity-list display name. */
export interface ArtifactActivityVM {
  activityId: string;
  name: string;
  row: ConstructionRow;
}

/** Compute progress percentage for a construction row (produced count / total). */
function progressOf(row: ConstructionRow): number {
  const total = row.produced?.length ?? 0;
  if (total === 0) return 0;
  const done = row.produced?.filter((a) => a.produced).length ?? 0;
  return Math.round((done / total) * 100);
}

function ListRow({
  onSelect,
  onComment,
  onKeyDown,
  onFocus,
  rowRef,
  focused,
  selected,
  t,
  vm,
}: {
  onSelect: () => void;
  onComment: () => void;
  onKeyDown: (e: React.KeyboardEvent) => void;
  onFocus: () => void;
  rowRef: (el: HTMLDivElement | null) => void;
  focused: boolean;
  selected: boolean;
  t: Tokens;
  vm: ArtifactActivityVM;
}): ReactNode {
  const pct = progressOf(vm.row);
  const artifactCount = vm.row.produced?.length ?? 0;
  // The ConstructionRow.status is a subset of BuildStatus; cast is safe.
  const status = vm.row.status as BuildStatus;

  return (
    <Box
      aria-label={`${vm.activityId} — ${vm.name}`}
      aria-selected={selected}
      data-testid={UI_IDENTIFIERS.Construction.artifactRow(vm.activityId)}
      ref={rowRef}
      role="option"
      sx={{
        px: 1.5,
        py: 1,
        cursor: 'pointer',
        borderLeft: `3px solid ${selected ? t.accent : 'transparent'}`,
        bgcolor: selected ? t.awaitingBg : 'transparent',
        borderBottom: `1px solid ${t.line}`,
        '& .commentable-row-action': { opacity: 0, transition: 'opacity 120ms' },
        '&:hover .commentable-row-action, &:focus-visible .commentable-row-action': { opacity: 1 },
        '&:hover': { bgcolor: selected ? t.awaitingBg : t.paperAlt },
        '&:focus-visible': {
          outline: `2px solid ${t.accent}`,
          outlineOffset: -2,
          '& .commentable-row-action': { opacity: 1 },
        },
      }}
      tabIndex={focused ? 0 : -1}
      onClick={onSelect}
      onFocus={onFocus}
      onKeyDown={onKeyDown}
    >
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
            color: selected ? t.awaitingFg : t.ink,
          }}
        >
          {vm.activityId}
        </Typography>
        <KindBadge kind={vm.row.kind} size="xs" t={t} />
        <Box sx={{ flexGrow: 1 }} />
        <StatusChip size="xs" status={status} t={t} />
        <Tooltip title="Comment on this activity">
          <IconButton
            aria-label={`Comment on ${vm.activityId} — ${vm.name}`}
            className="commentable-row-action"
            data-testid={UI_IDENTIFIERS.Comments.listItemComment(vm.activityId)}
            size="small"
            sx={{
              flexShrink: 0,
              width: 22,
              height: 22,
              color: t.accentText,
              bgcolor: t.accent,
              border: `1.5px solid ${t.line}`,
              borderRadius: 1,
              '&:hover': { bgcolor: t.accent2 },
            }}
            tabIndex={-1}
            onClick={(e) => {
              e.stopPropagation();
              onComment();
            }}
          >
            <ChatBubbleOutlineIcon sx={{ fontSize: 13 }} />
          </IconButton>
        </Tooltip>
      </Box>
      <Typography
        sx={{ fontFamily: t.body, fontSize: 12.5, color: t.ink, lineHeight: 1.25, mt: 0.25 }}
      >
        {vm.name}
      </Typography>
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, mt: 0.4 }}>
        <Box
          sx={{
            flexGrow: 1,
            height: 4,
            bgcolor: t.bg,
            border: `1px solid ${t.line}`,
            borderRadius: 99,
            overflow: 'hidden',
          }}
        >
          <Box
            sx={{
              width: `${pct.toString()}%`,
              height: '100%',
              bgcolor: kindColor(t, vm.row.kind).fg,
            }}
          />
        </Box>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted }}>{pct}%</Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted }}>
          · {artifactCount} artifacts
        </Typography>
      </Box>
    </Box>
  );
}

export function ArtifactActivityList({
  activities,
  onSelect,
  selectedId,
  t,
}: {
  activities: ArtifactActivityVM[];
  onSelect: (id: string) => void;
  selectedId: string;
  t: Tokens;
}): ReactNode {
  const { setAnchor } = useComments();
  const rowRefs = useRef<(HTMLDivElement | null)[]>([]);
  // Roving-tabindex focus index. Seed from the selected row so tabbing into the
  // list lands on the active activity; falls back to 0.
  const selectedIndex = activities.findIndex((vm) => vm.activityId === selectedId);
  const [focused, setFocused] = useState(selectedIndex >= 0 ? selectedIndex : 0);
  const clampedFocus = Math.min(focused, Math.max(activities.length - 1, 0));

  const moveTo = useCallback((index: number): void => {
    const clamped = Math.max(0, Math.min(index, rowRefs.current.length - 1));
    setFocused(clamped);
    rowRefs.current[clamped]?.focus();
  }, []);

  const arm = useCallback(
    (vm: ArtifactActivityVM): void => {
      setAnchor({
        kind: 'node',
        label: `${vm.activityId} — ${vm.name}`,
        source: 'Construction · activity',
        jsonPath: activityConstructionAnchor(vm.activityId),
      });
    },
    [setAnchor]
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent, vm: ArtifactActivityVM, index: number): void => {
      switch (e.key) {
        case 'ArrowDown':
          e.preventDefault();
          moveTo(index + 1);
          break;
        case 'ArrowUp':
          e.preventDefault();
          moveTo(index - 1);
          break;
        case 'Home':
          e.preventDefault();
          moveTo(0);
          break;
        case 'End':
          e.preventDefault();
          moveTo(activities.length - 1);
          break;
        case 'Enter':
        case ' ':
          e.preventDefault();
          onSelect(vm.activityId);
          break;
        case 'c':
        case 'C':
          e.preventDefault();
          arm(vm);
          break;
        default:
          break;
      }
    },
    [activities.length, moveTo, onSelect, arm]
  );

  return (
    <Paper sx={{ p: 0, overflow: 'hidden', position: { md: 'sticky' }, top: 8 }}>
      <Box
        sx={{
          px: 2,
          py: 1.1,
          bgcolor: t.paperAlt,
          borderBottom: `1.5px solid ${t.line}`,
        }}
      >
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
            letterSpacing: '0.08em',
            color: t.ink,
          }}
        >
          ALL ACTIVITIES · {activities.length}
        </Typography>
      </Box>
      <Box
        aria-label="All construction activities"
        role="listbox"
        sx={{ maxHeight: { md: 620 }, overflowY: 'auto', outline: 'none' }}
      >
        {activities.map((vm, index) => (
          <ListRow
            focused={index === clampedFocus}
            key={vm.activityId}
            rowRef={(el) => {
              rowRefs.current[index] = el;
            }}
            selected={vm.activityId === selectedId}
            t={t}
            vm={vm}
            onComment={() => {
              arm(vm);
            }}
            onFocus={() => {
              setFocused(index);
            }}
            onKeyDown={(e) => {
              onKeyDown(e, vm, index);
            }}
            onSelect={() => {
              onSelect(vm.activityId);
            }}
          />
        ))}
      </Box>
    </Paper>
  );
}
