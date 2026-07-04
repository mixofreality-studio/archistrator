/**
 * React-Flow node types for the C4-style deployment topology:
 *   • `deployGroup`     — a labelled deployment-node cluster (parent; e.g. an AWS
 *     region or a Kubernetes namespace), with an `×N` instance badge + description.
 *   • `deployContainer` — a container instance placed in a node: the primary C4
 *     "Container" box, with a "packages N components" affordance that expands (on
 *     hover or click) into the list of packaged System components, each coloured
 *     by its Method layer.
 *   • `deployInfra`     — a supporting infrastructure node (queue, LB, CDN, …):
 *     neutral styling, no layer colour.
 *   • `deployExternal`  — an external software system: dashed border.
 * All are presentational (the layout in DeploymentFlow sizes/positions them via
 * parentId + extent:'parent'); only `deployContainer` carries local UI state
 * (the expand/collapse of its packaged-components list).
 */
import { useState, type ReactNode } from 'react';
import type { NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import ButtonBase from '@mui/material/ButtonBase';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { layerColors, type Layer } from './flowLayout';

// Deployment-topology nodes are commentable: clicking any node arms a deployment
// anchor (handled by DeploymentFlow's onNodeClick, which reads `profile` + name
// from node.data). React Flow's own `selected` state is inert here (the graph is
// controlled without a change handler), so a pointer cursor — not a select
// toolbar — signals commentability. `profile` rides in each node's data.

export interface DeployGroupData {
  label: string;
  technology: string;
  description: string;
  instances: number;
  profile: string;
  [key: string]: unknown;
}

/** A packaged System component reference, coloured by Method layer. */
export interface DeployComponentRef {
  name: string;
  layer: Layer;
}

export interface DeployContainerData {
  name: string;
  technology: string;
  description: string;
  note: string;
  components: DeployComponentRef[];
  profile: string;
  [key: string]: unknown;
}

export interface DeployInfraData {
  name: string;
  technology: string;
  description: string;
  profile: string;
  [key: string]: unknown;
}

export interface DeployExternalData {
  name: string;
  technology: string;
  description: string;
  profile: string;
  [key: string]: unknown;
}

/** Two-line clamp used to keep boxes a uniform height. */
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
        cursor: 'pointer',
      }}
    >
      <Box sx={{ px: 1, py: 0.5, borderBottom: `1px solid ${t.line}`, bgcolor: t.paperAlt }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
          <Typography
            sx={{
              fontFamily: t.mono,
              fontWeight: 700,
              fontSize: 12,
              color: t.ink,
              lineHeight: 1.1,
            }}
          >
            {d.label}
          </Typography>
          {d.instances > 1 && (
            <Box sx={{ px: 0.6, py: 0.05, borderRadius: 1, bgcolor: t.accent }}>
              <Typography
                sx={{ fontFamily: t.mono, fontWeight: 700, fontSize: 9, color: t.accentText }}
              >
                ×{d.instances}
              </Typography>
            </Box>
          )}
        </Box>
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
      {d.description.length > 0 && (
        <Typography
          sx={{
            px: 1,
            py: 0.4,
            fontFamily: t.body,
            fontSize: 10,
            color: t.muted,
            lineHeight: 1.25,
            ...clamp2,
          }}
        >
          {d.description}
        </Typography>
      )}
    </Box>
  );
}

export function DeployContainerNode({ data, width, height }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployContainerData;
  const colors = layerColors(t);
  const [open, setOpen] = useState(false);
  const count = d.components.length;
  return (
    <Box
      sx={{ position: 'relative', width, height, cursor: 'pointer' }}
      onMouseEnter={() => {
        setOpen(true);
      }}
      onMouseLeave={() => {
        setOpen(false);
      }}
    >
      <Box
        sx={{
          width: '100%',
          height: '100%',
          display: 'flex',
          flexDirection: 'column',
          gap: 0.25,
          px: 1.25,
          py: 0.75,
          bgcolor: t.paperAlt,
          color: t.ink,
          border: `1.5px solid ${t.line}`,
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
        {d.technology.length > 0 && (
          <Typography
            sx={{
              fontFamily: t.mono,
              fontSize: 9,
              color: t.muted,
              letterSpacing: '0.05em',
            }}
          >
            [Container: {d.technology}]
          </Typography>
        )}
        {d.description.length > 0 && (
          <Typography
            sx={{
              fontFamily: t.body,
              fontSize: 10,
              color: t.muted,
              lineHeight: 1.25,
              ...clamp2,
            }}
          >
            {d.description}
          </Typography>
        )}
        {d.note.length > 0 && (
          <Typography
            sx={{ fontFamily: t.body, fontSize: 9.5, color: t.muted, fontStyle: 'italic' }}
          >
            {d.note}
          </Typography>
        )}
        {count > 0 && (
          <ButtonBase
            sx={{
              mt: 'auto',
              justifyContent: 'flex-start',
              fontFamily: t.mono,
              fontSize: 9,
              fontWeight: 700,
              color: t.accent2,
              letterSpacing: '0.04em',
            }}
            onClick={() => {
              setOpen((o) => !o);
            }}
          >
            {open ? '▾' : '▸'} packages {count} component{count === 1 ? '' : 's'}
          </ButtonBase>
        )}
      </Box>
      {open && count > 0 ? (
        <Box
          sx={{
            position: 'absolute',
            top: '100%',
            left: 0,
            mt: 0.5,
            width: Math.max(typeof width === 'number' ? width : 0, 180),
            zIndex: 30,
            display: 'flex',
            flexDirection: 'column',
            gap: 0.35,
            p: 0.75,
            bgcolor: t.paper,
            border: `1.5px solid ${t.line}`,
            borderRadius: 2,
            boxShadow: '0 4px 14px rgba(0,0,0,0.28)',
          }}
        >
          {d.components.map((c) => (
            <Box key={c.name} sx={{ display: 'flex', alignItems: 'center', gap: 0.6 }}>
              <Box
                sx={{
                  width: 8,
                  height: 8,
                  flexShrink: 0,
                  borderRadius: '50%',
                  bgcolor: colors[c.layer],
                }}
              />
              <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.ink, lineHeight: 1.2 }}>
                {c.name}
              </Typography>
            </Box>
          ))}
        </Box>
      ) : null}
    </Box>
  );
}

export function DeployInfraNode({ data, width, height }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployInfraData;
  return (
    <Box
      sx={{
        position: 'relative',
        width,
        height,
        px: 1.25,
        py: 0.75,
        bgcolor: t.paperAlt,
        color: t.ink,
        border: `1.5px solid ${t.muted}`,
        borderRadius: 2,
        overflow: 'hidden',
        cursor: 'pointer',
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11.5,
          lineHeight: 1.15,
          wordBreak: 'break-word',
          ...clamp2,
        }}
      >
        {d.name}
      </Typography>
      {d.technology.length > 0 && (
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 9,
            color: t.muted,
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
          }}
        >
          {d.technology}
        </Typography>
      )}
      {d.description.length > 0 && (
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
          {d.description}
        </Typography>
      )}
    </Box>
  );
}

export function DeployExternalNode({ data, width, height }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployExternalData;
  return (
    <Box
      sx={{
        position: 'relative',
        width,
        height,
        px: 1.25,
        py: 0.75,
        bgcolor: t.paper,
        color: t.muted,
        border: `1.5px dashed ${t.muted}`,
        borderRadius: 2,
        overflow: 'hidden',
        cursor: 'pointer',
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11.5,
          lineHeight: 1.15,
          color: t.ink,
          wordBreak: 'break-word',
          ...clamp2,
        }}
      >
        {d.name}
      </Typography>
      {d.technology.length > 0 && (
        <Typography
          sx={{
            fontFamily: t.mono,
            fontSize: 9,
            color: t.muted,
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
          }}
        >
          {d.technology}
        </Typography>
      )}
      {d.description.length > 0 && (
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
          {d.description}
        </Typography>
      )}
    </Box>
  );
}
