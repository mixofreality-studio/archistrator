/**
 * A non-interactive React-Flow node that paints the faint background band and
 * header label for a single swim-lane column. Rendered behind the activity nodes
 * (lowest z-index, selectable/draggable disabled) so the diagram reads as a set
 * of role columns. Colors come entirely from theme tokens via SwimlaneData.
 */
import type { ReactNode } from 'react';
import type { NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../theme/ThemeContext';

export interface SwimlaneData {
  lane: string;
  color: string;
  band: string;
  width: number;
  height: number;
  headerHeight: number;
  [key: string]: unknown;
}

export function SwimlaneBackground({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as SwimlaneData;

  return (
    <Box
      sx={{
        width: d.width,
        height: d.height,
        bgcolor: d.band,
        borderLeft: `1px dashed ${t.line}`,
        borderRight: `1px dashed ${t.line}`,
      }}
    >
      <Box
        sx={{
          height: d.headerHeight,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          px: 1,
          borderBottom: `1.5px solid ${d.color}`,
          bgcolor: t.paper,
        }}
      >
        <Typography
          sx={{
            fontFamily: t.mono,
            fontWeight: 700,
            fontSize: 11,
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
            color: d.color,
            textAlign: 'center',
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            maxWidth: '100%',
          }}
        >
          {d.lane}
        </Typography>
      </Box>
    </Box>
  );
}
