/**
 * A React-Flow node for the activity diagrams. Renders a real UML shape per kind:
 * start = filled dot, end = ringed final node, action/loop/goto/interruptEdge =
 * rounded card, decision/switch = diamond, merge = smaller diamond, fork/join =
 * synchronization bar, note = sticky note. Lane-colored. Selecting a node reveals
 * a toolbar that arms an activity-node comment anchor
 * (`$.decisions[uc].useCase.activity.nodes[id=…]`).
 */
import type { ReactNode } from 'react';
import { Handle, Position, NodeToolbar, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import type { ActivityNodeKind } from '../../api/models';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { useComments } from '../comments/CommentContext';
import { NODE_DIMS } from './nodeDims';


export interface ActivityNodeData {
  label: string;
  lane: string;
  kind: ActivityNodeKind;
  color: string;
  source: string;
  jsonPath: string;
  [key: string]: unknown;
}

const hiddenHandle = { opacity: 0 } as const;

function Handles(): ReactNode {
  return (
    <>
      <Handle position={Position.Top} style={hiddenHandle} type="target" />
      <Handle id="l" position={Position.Left} style={hiddenHandle} type="target" />
      <Handle position={Position.Bottom} style={hiddenHandle} type="source" />
      <Handle id="r" position={Position.Right} style={hiddenHandle} type="source" />
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
        <Typography sx={{ fontFamily: t.mono, fontSize: 9.5, color: t.muted, whiteSpace: 'nowrap' }}>
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
  const { setAnchor } = useComments();
  const d = data as ActivityNodeData;

  let shape: ReactNode;
  switch (d.kind) {
    case 'start':
      shape = Dot(t, d, selected, false);
      break;
    case 'end':
      shape = Dot(t, d, selected, true);
      break;
    case 'decision':
    case 'switch':
      shape = Diamond(t, d, selected, false);
      break;
    case 'merge':
      shape = Diamond(t, d, selected, true);
      break;
    case 'fork':
    case 'join':
      shape = Bar(t, d, selected);
      break;
    case 'note':
      shape = Note(t, d, selected);
      break;
    case 'action':
    case 'loop':
    case 'goto':
    case 'interruptEdge':
    case 'swimLane':
    default:
      shape = Card(t, d, selected);
      break;
  }

  return (
    <>
      <NodeToolbar isVisible={selected} offset={8} position={Position.Right}>
        <Button
          size="small"
          startIcon={<ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />}
          sx={{ py: 0.25, color: t.accentText, bgcolor: t.accent, border: `1.5px solid ${t.line}`, '&:hover': { bgcolor: t.accent2 } }}
          onClick={() => {
            setAnchor({ kind: 'node', label: d.label, source: d.source, jsonPath: d.jsonPath });
          }}
        >
          Comment
        </Button>
      </NodeToolbar>
      <Handles />
      {shape}
    </>
  );
}
