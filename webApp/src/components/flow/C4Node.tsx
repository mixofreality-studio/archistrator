/**
 * A React-Flow node for the C4 component view. Colored by Method layer; selecting
 * it reveals a toolbar that arms a component comment anchor (`$.components[id=…]`).
 */
import { useState, type FocusEvent, type KeyboardEvent, type ReactNode } from 'react';
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
  /** Selection carried through data (xyflow's built-in controlled selection is inert
   *  here) — drives the Comment toolbar + accent ring. */
  isSelected?: boolean;
  [key: string]: unknown;
}

export function C4Node({ data, selected }: NodeProps): ReactNode {
  const t = useTokens();
  const { setAnchor, enabled } = useComments();
  const d = data as C4NodeData;
  const hasDetail = d.encapsulates.length > 0;
  const showPreview = d.showEncapsulates !== false && hasDetail;
  // Selecting a node = focusing it (click focuses the Box; keyboard Tab does too).
  // xyflow captures node pointer events, so onNodeClick is unreliable here — driving
  // the Comment toolbar + ring off the Box's own focus is robust for mouse AND keyboard.
  const [focused, setFocused] = useState(false);
  const isSelected = focused || d.isSelected === true || selected;
  // Keep the toolbar up while focus moves INTO it (its Comment button lives in a
  // NodeToolbar portal) — otherwise the blur would unmount the button mid-click.
  const onBlur = (e: FocusEvent): void => {
    const to = e.relatedTarget as HTMLElement | null;
    if (to?.closest('.react-flow__node-toolbar') != null) return;
    setFocused(false);
  };

  const armComment = (): void => {
    setAnchor({
      kind: 'node',
      label: d.name,
      source: 'Architecture · C4',
      jsonPath: componentAnchor(d.componentId),
    });
  };
  // Enter / 'c' arms a comment on a keyboard-focused node — the same convention as
  // CommentableList — so the diagram is operable without a mouse. Comment action is
  // gated on `enabled`; focus + label work regardless (read-only home base included).
  const onKeyDown = (e: KeyboardEvent): void => {
    if (enabled && (e.key === 'Enter' || e.key === 'c' || e.key === 'C')) {
      e.preventDefault();
      armComment();
    }
  };

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
        border: `1.5px solid ${isSelected ? t.accent : t.line}`,
        borderTop: `4px solid ${d.color}`,
        borderRadius: 4,
        boxShadow: isSelected ? `0 0 0 2px ${t.accent}` : 'none',
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 12.5,
          lineHeight: 1.2,
          wordBreak: 'break-word',
        }}
      >
        {d.name}
      </Typography>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontSize: 9,
          color: d.color,
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
        }}
      >
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
        <NodeToolbar isVisible={isSelected} offset={6} position={Position.Top}>
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
      {/* Focusable, labeled wrapper: makes the node reachable by keyboard/AT with a
          visible focus ring + an accessible name (component + layer), and handles the
          Enter/'c' comment shortcut. Comment arming is gated on `enabled`; focus and
          label are always on (read-only home base still keyboard-navigable). */}
      <Box
        aria-label={
          enabled
            ? `${d.name}, ${d.layer} layer. Press C to comment.`
            : `${d.name}, ${d.layer} layer`
        }
        role="button"
        sx={{
          borderRadius: 4,
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
              arrow: {
                sx: { color: t.paperAlt, '&::before': { border: `1.5px solid ${t.line}` } },
              },
            }}
            title={
              <>
                <Typography
                  sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 11.5, color: t.ink }}
                >
                  {d.name}
                </Typography>
                <Typography
                  sx={{
                    fontFamily: t.mono,
                    fontSize: 8.5,
                    color: d.color,
                    letterSpacing: '0.08em',
                    textTransform: 'uppercase',
                    mb: 0.5,
                  }}
                >
                  {d.layer}
                </Typography>
                <Typography
                  sx={{ fontFamily: t.body, fontSize: 11.5, color: t.muted, lineHeight: 1.4 }}
                >
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
      </Box>
      <Handle id="b" position={Position.Bottom} style={{ opacity: 0 }} type="source" />
      <Handle id="sr" position={Position.Right} style={{ opacity: 0 }} type="source" />
    </>
  );
}
