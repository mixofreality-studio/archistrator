/**
 * A React-Flow node for the C4 component view. Colored by Method layer; selecting
 * it reveals a toolbar that arms a component comment anchor (`$.components[id=…]`).
 */
import type { ReactNode } from 'react';
import { Handle, Position, NodeToolbar, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Typography from '@mui/material/Typography';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { useTokens } from '../../theme/ThemeContext';
import { useComments, componentAnchor } from '../comments/CommentContext';

export interface C4NodeData {
  componentId: string;
  name: string;
  layer: string;
  encapsulates: string;
  color: string;
  [key: string]: unknown;
}

export function C4Node({ data, selected }: NodeProps): ReactNode {
  const t = useTokens();
  const { setAnchor } = useComments();
  const d = data as C4NodeData;
  return (
    <>
      <Handle position={Position.Top} style={{ opacity: 0 }} type="target" />
      <NodeToolbar isVisible={selected} offset={6} position={Position.Top}>
        <Button
          size="small"
          startIcon={<ChatBubbleOutlineIcon sx={{ fontSize: 14 }} />}
          sx={{ py: 0.25, color: t.accentText, bgcolor: t.accent, border: `1.5px solid ${t.line}`, '&:hover': { bgcolor: t.accent2 } }}
          onClick={() => {
            setAnchor({
              kind: 'node',
              label: d.name,
              source: 'Architecture · C4',
              jsonPath: componentAnchor(d.componentId),
            });
          }}
        >
          Comment
        </Button>
      </NodeToolbar>
      <Box
        sx={{
          width: 188,
          px: 1.5,
          py: 1,
          bgcolor: t.paperAlt,
          color: t.ink,
          border: `1.5px solid ${selected ? t.accent : t.line}`,
          borderTop: `4px solid ${d.color}`,
          borderRadius: 4,
          boxShadow: selected ? `0 0 0 2px ${t.accent}` : 'none',
        }}
      >
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12.5, lineHeight: 1.2, wordBreak: 'break-word' }}>
          {d.name}
        </Typography>
        <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: d.color, letterSpacing: '0.08em', textTransform: 'uppercase' }}>
          {d.layer}
        </Typography>
        {d.encapsulates.length > 0 && (
          <Typography sx={{ fontFamily: t.body, fontSize: 11, color: t.muted, mt: 0.25 }}>
            {d.encapsulates}
          </Typography>
        )}
      </Box>
      <Handle position={Position.Bottom} style={{ opacity: 0 }} type="source" />
    </>
  );
}
