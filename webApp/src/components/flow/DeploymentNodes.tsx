/**
 * Two React-Flow node types for the deployment topology:
 *   • `deployGroup`    — a labelled cluster/namespace container (parent node).
 *   • `deployInstance` — a System component instance, coloured by its Method layer.
 * Both are presentational (the layout in DeploymentFlow sizes/positions them via
 * parentId + extent:'parent'); instances are non-interactive leaves.
 */
import type { ReactNode } from 'react';
import type { NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../theme/ThemeContext';

export interface DeployGroupData {
  label: string;
  technology: string;
  [key: string]: unknown;
}

export interface DeployInstanceData {
  name: string;
  layerLabel: string;
  color: string;
  note: string;
  [key: string]: unknown;
}

export function DeployGroupNode({ data, width, height }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployGroupData;
  return (
    <Box
      sx={{
        width,
        height,
        bgcolor: t.paper,
        border: `1.5px dashed ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
      }}
    >
      <Box sx={{ px: 1, py: 0.5, borderBottom: `1px solid ${t.line}`, bgcolor: t.paperAlt }}>
        <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink, lineHeight: 1.1 }}>
          {d.label}
        </Typography>
        {d.technology.length > 0 && (
          <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: t.muted, letterSpacing: '0.08em', textTransform: 'uppercase' }}>
            {d.technology}
          </Typography>
        )}
      </Box>
    </Box>
  );
}

export function DeployInstanceNode({ data }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployInstanceData;
  return (
    <Box
      sx={{
        width: 168,
        px: 1.25,
        py: 0.75,
        bgcolor: t.paperAlt,
        color: t.ink,
        border: `1.5px solid ${t.line}`,
        borderLeft: `4px solid ${d.color}`,
        borderRadius: 2,
      }}
    >
      <Typography sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, lineHeight: 1.2, wordBreak: 'break-word' }}>
        {d.name}
      </Typography>
      <Typography sx={{ fontFamily: t.mono, fontSize: 9, color: d.color, letterSpacing: '0.08em', textTransform: 'uppercase' }}>
        {d.layerLabel}
      </Typography>
      {d.note.length > 0 && (
        <Typography sx={{ fontFamily: t.body, fontSize: 10.5, color: t.muted, mt: 0.25 }}>
          {d.note}
        </Typography>
      )}
    </Box>
  );
}
