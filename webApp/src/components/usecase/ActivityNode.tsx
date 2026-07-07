/**
 * A React-Flow node for the activity diagrams. Renders a real UML shape per kind:
 * start = filled dot, end = ringed final node, action/loop/goto/interruptEdge =
 * rounded card, decision/switch = diamond, merge = smaller diamond, fork/join =
 * synchronization bar, note = sticky note. Lane-colored. Selecting a node reveals
 * a toolbar that arms an activity-node comment anchor
 * (`$.decisions[uc].useCase.activity.nodes[id=…]`).
 */
import { useState, type FocusEvent, type KeyboardEvent, type ReactNode } from 'react';
import { Handle, Position, NodeToolbar, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import type { ActivityNodeKind } from '../../contracts/models';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { useComments } from '../comments/CommentContext';
import { UI_IDENTIFIERS } from '../../utilities/constants/UIIdentifiers';
import { NODE_DIMS } from './nodeDims';

export interface ActivityNodeData {
  label: string;
  lane: string;
  kind: ActivityNodeKind;
  color: string;
  source: string;
  jsonPath: string;
  /** Selection carried through data (xyflow's controlled selection is inert here) —
   *  drives the Comment toolbar + accent ring. */
  isSelected?: boolean;
  /** Walkthrough "you-are-here" current step — exactly one node per map carries it.
   *  Publishes a stable testid so the per-step camera focus is black-box assertable. */
  isCurrent?: boolean;
  [key: string]: unknown;
}

const hiddenHandle = {
  opacity: 0,
  width: 1,
  height: 1,
  minWidth: 0,
  minHeight: 0,
  border: 'none',
} as const;

/**
 * Invisible connection points at each vertex. Every side carries both a target
 * and a source handle (co-located, so which one is used is chosen per-edge in
 * ActivityFlow by geometry): forward flow enters the top and leaves the bottom;
 * a decision's side branches leave left/right; loop-back edges enter/leave a side.
 * Stable ids let ActivityFlow pin each edge to a specific vertex so connection
 * points stay consistent across every diagram.
 */
function Handles(): ReactNode {
  return (
    <>
      {/* targets */}
      <Handle id="t" position={Position.Top} style={hiddenHandle} type="target" />
      <Handle id="lt" position={Position.Left} style={hiddenHandle} type="target" />
      <Handle id="rt" position={Position.Right} style={hiddenHandle} type="target" />
      <Handle id="bt" position={Position.Bottom} style={hiddenHandle} type="target" />
      {/* sources */}
      <Handle id="b" position={Position.Bottom} style={hiddenHandle} type="source" />
      <Handle id="ls" position={Position.Left} style={hiddenHandle} type="source" />
      <Handle id="rs" position={Position.Right} style={hiddenHandle} type="source" />
      <Handle id="ts" position={Position.Top} style={hiddenHandle} type="source" />
    </>
  );
}

function Dot(t: Tokens, d: ActivityNodeData, selected: boolean, ring: boolean): ReactNode {
  const dim = ring ? NODE_DIMS.end : NODE_DIMS.start;
  return (
    <Box
      sx={{
        width: dim.w,
        height: dim.h,
        borderRadius: '50%',
        bgcolor: d.color,
        border: `${ring ? '3px' : '1.5px'} solid ${selected ? t.accent : t.line}`,
        boxShadow: ring ? `inset 0 0 0 3px ${t.bg}` : selected ? `0 0 0 2px ${t.accent}` : 'none',
      }}
    />
  );
}

function Bar(t: Tokens, d: ActivityNodeData, selected: boolean): ReactNode {
  const dim = NODE_DIMS.fork;
  return (
    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
      <Box
        sx={{
          width: dim.w,
          height: dim.h,
          bgcolor: t.ink,
          border: `1.5px solid ${selected ? t.accent : t.line}`,
          borderRadius: 0.5,
          boxShadow: selected ? `0 0 0 2px ${t.accent}` : 'none',
        }}
      />
      {d.label.length > 0 ? (
        <Typography
          sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, whiteSpace: 'nowrap' }}
        >
          {d.label}
        </Typography>
      ) : null}
    </Box>
  );
}

function Diamond(t: Tokens, d: ActivityNodeData, selected: boolean, merge: boolean): ReactNode {
  const dim = merge ? NODE_DIMS.merge : NODE_DIMS.decision;
  return (
    <Box sx={{ position: 'relative', width: dim.w, height: dim.h }}>
      <Box
        sx={{
          position: 'absolute',
          inset: 0,
          transform: 'rotate(45deg)',
          bgcolor: t.paperAlt,
          border: `1.5px solid ${selected ? t.accent : t.line}`,
          borderLeftColor: d.color,
          borderTopColor: d.color,
          borderRadius: 1,
          boxShadow: selected ? `0 0 0 2px ${t.accent}` : 'none',
        }}
      />
      <Box
        sx={{
          position: 'absolute',
          inset: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          px: merge ? 0.5 : 1.25,
          textAlign: 'center',
        }}
      >
        <Typography
          sx={{
            fontFamily: t.body,
            fontWeight: 600,
            fontSize: merge ? 9.5 : 11,
            lineHeight: 1.15,
            color: t.ink,
          }}
        >
          {d.label}
        </Typography>
      </Box>
    </Box>
  );
}

function Card(t: Tokens, d: ActivityNodeData, selected: boolean): ReactNode {
  return (
    <Box
      sx={{
        width: NODE_DIMS.action.w,
        minHeight: NODE_DIMS.action.h,
        px: 1.75,
        py: 1.1,
        display: 'flex',
        alignItems: 'center',
        bgcolor: t.paperAlt,
        color: t.ink,
        border: `1.5px solid ${selected ? t.accent : t.line}`,
        borderLeft: `5px solid ${d.color}`,
        borderRadius: 4,
        boxShadow: selected ? `0 0 0 2px ${t.accent}` : 'none',
      }}
    >
      <Box sx={{ minWidth: 0 }}>
        <Typography sx={{ fontFamily: t.body, fontWeight: 600, fontSize: 13, lineHeight: 1.25 }}>
          {d.label}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, opacity: 0.85 }}>
          {d.lane}
        </Typography>
      </Box>
    </Box>
  );
}

function Note(t: Tokens, d: ActivityNodeData, selected: boolean): ReactNode {
  return (
    <Box
      sx={{
        position: 'relative',
        width: NODE_DIMS.note.w,
        minHeight: NODE_DIMS.note.h,
        px: 1.5,
        py: 1.1,
        bgcolor: t.awaitingBg,
        color: t.ink,
        border: `1.5px solid ${selected ? t.accent : t.line}`,
        // Folded top-right corner.
        clipPath: 'polygon(0 0, calc(100% - 14px) 0, 100% 14px, 100% 100%, 0 100%)',
        boxShadow: selected ? `0 0 0 2px ${t.accent}` : 'none',
      }}
    >
      <Typography sx={{ fontFamily: t.body, fontStyle: 'italic', fontSize: 12, lineHeight: 1.3 }}>
        {d.label}
      </Typography>
    </Box>
  );
}

export function ActivityNode({ data, selected }: NodeProps): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled } = useComments();
  const d = data as ActivityNodeData;
  // Selecting a step = focusing it (click focuses the Box; keyboard Tab does too).
  // xyflow captures node pointer events, so onNodeClick is unreliable — driving the
  // Comment toolbar + ring off the Box's own focus is robust for mouse AND keyboard.
  const [focused, setFocused] = useState(false);
  const isSelected = focused || d.isSelected === true || selected;
  // Keep the toolbar up while focus moves INTO its (portaled) Comment button.
  const onBlur = (e: FocusEvent): void => {
    const to = e.relatedTarget as HTMLElement | null;
    if (to?.closest('.react-flow__node-toolbar') != null) return;
    setFocused(false);
  };

  const armComment = (): void => {
    setAnchor({ kind: 'node', label: d.label, source: d.source, jsonPath: d.jsonPath });
  };
  // Enter / 'c' arms a comment on a keyboard-focused step (CommentableList convention),
  // so the activity diagram is operable without a mouse. Gated on `enabled`.
  const onKeyDown = (e: KeyboardEvent): void => {
    if (enabled && (e.key === 'Enter' || e.key === 'c' || e.key === 'C')) {
      e.preventDefault();
      armComment();
    }
  };
  // A concise accessible name: the step label, its UML kind, and its lane/role.
  const ariaLabel = `${d.label.length > 0 ? d.label : d.kind}, ${d.kind} in ${d.lane}`;

  let shape: ReactNode;
  switch (d.kind) {
    case 'start':
      shape = Dot(t, d, isSelected, false);
      break;
    case 'end':
      shape = Dot(t, d, isSelected, true);
      break;
    case 'decision':
    case 'switch':
      shape = Diamond(t, d, isSelected, false);
      break;
    case 'merge':
      shape = Diamond(t, d, isSelected, true);
      break;
    case 'fork':
    case 'join':
      shape = Bar(t, d, isSelected);
      break;
    case 'note':
      shape = Note(t, d, isSelected);
      break;
    case 'action':
    case 'loop':
    case 'goto':
    case 'interruptEdge':
    case 'swimLane':
    default:
      shape = Card(t, d, isSelected);
      break;
  }

  return (
    <>
      {enabled ? (
        <NodeToolbar isVisible={isSelected} offset={8} position={Position.Right}>
          <Button
            size="small"
            startIcon={<ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />}
            sx={{
              py: 0.25,
              color: t.accentText,
              bgcolor: t.accent,
              border: `1.5px solid ${t.line}`,
              '&:hover': { bgcolor: t.accent2 },
            }}
            onClick={armComment}
          >
            Comment
          </Button>
        </NodeToolbar>
      ) : null}
      <Handles />
      {/* Focusable, labeled wrapper: keyboard/AT reach the step with a visible focus
          ring + accessible name, and Enter/'c' arms its comment. Focus + label are
          always on; comment action only where commenting is enabled. */}
      <Box
        aria-label={enabled ? `${ariaLabel}. Press C to comment.` : ariaLabel}
        data-testid={
          d.isCurrent === true ? UI_IDENTIFIERS.UseCaseCarousel.WALKTHROUGH_CURRENT_NODE : undefined
        }
        role="button"
        sx={{
          display: 'inline-flex',
          borderRadius: 1.5,
          outline: 'none',
          '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 3 },
        }}
        tabIndex={0}
        onBlur={onBlur}
        onFocus={() => {
          setFocused(true);
        }}
        onKeyDown={onKeyDown}
      >
        {shape}
      </Box>
    </>
  );
}
