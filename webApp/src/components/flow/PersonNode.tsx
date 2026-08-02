/**
 * A React-Flow node for a PERSON participating in a realized call chain — a use
 * case's actor (adapters.PersonView), not a System component. Drawn as the UML
 * actor idiom (circle head over a rounded-shoulders body) beside the actor's name
 * and role, in the neutral person-lane tone.
 *
 * Deliberately thinner than C4Node: an actor holds no volatility, no layer tag and
 * no comment anchor (there is no `$.components[id=…]` to point at), so the node is
 * a labelled, focusable read-only glyph — the EdgeFindingBadge idiom — rather than
 * a role="button" card.
 */
import type { ReactNode } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { NODE_W } from './flowLayout';

export interface PersonNodeData {
  /** The actor id — also its display name (actors carry no separate name). */
  personId: string;
  role: string;
  /** The person-lane colour (layerColors().person). */
  color: string;
  [key: string]: unknown;
}

export function PersonNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as PersonNodeData;
  const hasRole = d.role.length > 0;
  return (
    <>
      <Handle id="t" position={Position.Top} style={{ opacity: 0 }} type="target" />
      <Handle id="tl" position={Position.Left} style={{ opacity: 0 }} type="target" />
      <Box
        aria-label={hasRole ? `${d.personId}, person: ${d.role}` : `${d.personId}, person`}
        role="img"
        sx={{
          width: NODE_W,
          display: 'flex',
          alignItems: 'center',
          gap: 1.25,
          px: 1.5,
          py: 1,
          bgcolor: t.paperAlt,
          color: t.ink,
          border: `1.5px solid ${t.line}`,
          borderTop: `4px solid ${d.color}`,
          borderRadius: 4,
          outline: 'none',
          '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 3 },
        }}
        tabIndex={0}
      >
        {/* UML actor glyph: head + shoulders, stacked. */}
        <Box
          sx={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            flexShrink: 0,
            width: 26,
          }}
        >
          <Box
            sx={{
              width: 12,
              height: 12,
              borderRadius: '50%',
              border: `2px solid ${d.color}`,
            }}
          />
          <Box
            sx={{
              width: 22,
              height: 11,
              mt: '2px',
              borderTop: `2px solid ${d.color}`,
              borderLeft: `2px solid ${d.color}`,
              borderRight: `2px solid ${d.color}`,
              borderTopLeftRadius: 11,
              borderTopRightRadius: 11,
            }}
          />
        </Box>
        <Box sx={{ minWidth: 0 }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 12.5,
              lineHeight: 1.2,
              wordBreak: 'break-word',
            }}
          >
            {d.personId}
          </Typography>
          {hasRole ? (
            <Typography
              sx={{
                fontFamily: t.body,
                fontSize: 11,
                color: t.muted,
                // Clamped like the C4 node's volatility preview so a long role
                // never grows the node past its row pitch.
                display: '-webkit-box',
                WebkitLineClamp: 2,
                WebkitBoxOrient: 'vertical',
                overflow: 'hidden',
              }}
            >
              {d.role}
            </Typography>
          ) : null}
        </Box>
      </Box>
      <Handle id="b" position={Position.Bottom} style={{ opacity: 0 }} type="source" />
      <Handle id="sr" position={Position.Right} style={{ opacity: 0 }} type="source" />
    </>
  );
}
