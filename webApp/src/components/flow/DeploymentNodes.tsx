/**
 * Three React-Flow node types for the deployment topology:
 *   • `deployGroup`      — a labelled cluster/namespace container (parent node).
 *   • `deployLayerLabel` — a Method-layer row label in the container's left gutter.
 *   • `deployInstance`   — a System component instance, coloured by its Method layer.
 * All are presentational (the layout in DeploymentFlow sizes/positions them via
 * parentId + extent:'parent'); instances and labels are non-interactive leaves.
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

export interface DeployLayerLabelData {
  label: string;
  color: string;
  [key: string]: unknown;
}

/** Two-line clamp used to keep instance rows a uniform height. */
const clamp2 = {
  display: '-webkit-box',
  WebkitLineClamp: 2,
  WebkitBoxOrient: 'vertical' as const,
  overflow: 'hidden',
};

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
        <Typography
          sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 12, color: t.ink, lineHeight: 1.1 }}
        >
          {d.label}
        </Typography>
        {d.technology.length > 0 && (
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 9,
              color: t.muted,
              letterSpacing: '0.08em',
              textTransform: 'uppercase',
            }}
          >
            {d.technology}
          </Typography>
        )}
      </Box>
    </Box>
  );
}

export function DeployInstanceNode({ data, width, height }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployInstanceData;
  return (
    <Box
      sx={{
        width,
        height,
        px: 1.25,
        py: 0.75,
        bgcolor: t.paperAlt,
        color: t.ink,
        border: `1.5px solid ${t.line}`,
        borderLeft: `4px solid ${d.color}`,
        borderRadius: 2,
        overflow: 'hidden',
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 12,
          lineHeight: 1.15,
          wordBreak: 'break-word',
          ...clamp2,
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
        {d.layerLabel}
      </Typography>
      {d.note.length > 0 && (
        <Typography
          sx={{
            fontFamily: t.body,
            fontSize: 10,
            color: t.muted,
            mt: 0.25,
            lineHeight: 1.25,
            ...clamp2,
          }}
        >
          {d.note}
        </Typography>
      )}
    </Box>
  );
}

/** Method-layer row label rendered in a container's left gutter. */
export function DeployLayerLabelNode({ data, width, height }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployLayerLabelData;
  return (
    <Box sx={{ width, height, display: 'flex', alignItems: 'center' }}>
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 10,
          color: d.color,
          letterSpacing: '0.1em',
          textTransform: 'uppercase',
          lineHeight: 1.15,
        }}
      >
        {d.label}
      </Typography>
    </Box>
  );
}
