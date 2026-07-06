/**
 * A React-Flow node for the C4 component view. Colored by Method layer; selecting
 * it reveals a toolbar that arms a component comment anchor (`$.components[id=…]`).
 */
import type { ReactNode } from 'react';
import { Handle, Position, NodeToolbar, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Button from '@mui/material/Button';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import ChatBubbleOutlineIcon from '@mui/icons-material/ChatBubbleOutline';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { useComments, componentAnchor } from '../comments/CommentContext';

export interface C4NodeData {
  componentId: string;
  name: string;
  layer: string;
  encapsulates: string;
  /** Show the clamped volatility preview in the node body (Static / focus lenses).
   *  Off for the Dynamic step-through. Undefined is treated as on. */
  showEncapsulates?: boolean;
  color: string;
  [key: string]: unknown;
}

export function C4Node({ data, selected }: NodeProps): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled } = useComments();
  const d = data as C4NodeData;
  const hasDetail = d.encapsulates.length > 0;
  const showPreview = d.showEncapsulates !== false && hasDetail;

  // The compact card: name + layer tag, and (Static / focus lenses only) a 2-line
  // clamped volatility preview. Full prose is never rendered inside the node — it is
  // reachable via the hover-focus card below, per the house diagram convention.
  const card = (
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
      {showPreview ? (
        <Typography
          sx={{
            fontFamily: t.body,
            fontSize: 11,
            color: t.muted,
            mt: 0.25,
            // Clamp to 2 lines so node heights stay stable and neighbours never overlap;
            // the full text lives in the hover-focus card.
            display: '-webkit-box',
            WebkitLineClamp: 2,
            WebkitBoxOrient: 'vertical',
            overflow: 'hidden',
          }}
        >
          {d.encapsulates}
        </Typography>
      ) : null}
    </Box>
  );

  return (
    <>
      <Handle id="t" position={Position.Top} style={{ opacity: 0 }} type="target" />
      <Handle id="tl" position={Position.Left} style={{ opacity: 0 }} type="target" />
      {enabled ? (
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
      ) : null}
      {hasDetail ? (
        <Tooltip
          arrow
          enterDelay={200}
          placement="top"
          slotProps={{
            tooltip: {
              sx: {
                bgcolor: t.paperAlt,
                color: t.ink,
                border: `1.5px solid ${t.line}`,
                maxWidth: 260,
                boxShadow: 3,
                px: 1.25,
                py: 1,
              },
            },
            arrow: { sx: { color: t.paperAlt, '&::before': { border: `1.5px solid ${t.line}` } } },
          }}
          title={
            <>
              <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11.5, color: t.ink }}>
                {d.name}
              </Typography>
              <Typography sx={{ fontFamily: t.mono, fontSize: 8.5, color: d.color, letterSpacing: '0.08em', textTransform: 'uppercase', mb: 0.5 }}>
                {d.layer}
              </Typography>
              <Typography sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.4 }}>
                {d.encapsulates}
              </Typography>
            </>
          }
        >
          {card}
        </Tooltip>
      ) : (
        card
      )}
      <Handle id="b" position={Position.Bottom} style={{ opacity: 0 }} type="source" />
      <Handle id="sr" position={Position.Right} style={{ opacity: 0 }} type="source" />
    </>
  );
}
