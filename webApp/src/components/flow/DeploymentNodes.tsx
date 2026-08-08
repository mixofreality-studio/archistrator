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
import { useState, type KeyboardEvent, type ReactNode } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import Box from '@mui/material/Box';
import ButtonBase from '@mui/material/ButtonBase';
import Typography from '@mui/material/Typography';
import { useTokens } from '../../utilities/theme/ThemeContext';
import { useComments, deploymentAnchor } from '../comments/CommentContext';
import { layerColors, type Layer } from './flowLayout';

// Deployment-topology nodes are commentable AND keyboard-operable: a pointer click
// arms a deployment anchor (handled by DeploymentFlow's onNodeClick, which reads
// `profile` + name from node.data), and every node's root box is a focusable,
// labelled `button` so keyboard/AT users can Tab to it and press Enter/'c' to arm the
// same anchor — the C4Node convention (P1 a11y). React Flow's own `selected` state is
// inert here (the graph is controlled without a change handler), so a pointer cursor +
// focus ring — not a select toolbar — signals commentability. `profile` rides in data.

/**
 * The focusable/keyboard-comment props shared by every deployment node's root box:
 * a labelled `button` role with a visible focus ring, arming the deployment comment
 * anchor on Enter/'c' (gated on an active CommentContext). Mirrors C4Node so the
 * deployment diagram is operable without a mouse.
 */
function useDeployNodeA11y(
  name: string,
  profile: string
): {
  role: 'button';
  tabIndex: 0;
  'aria-label': string;
  onKeyDown: (e: KeyboardEvent) => void;
} {
  const { setAnchor, enabled } = useComments();
  const onKeyDown = (e: KeyboardEvent): void => {
    if (!enabled) return;
    if (e.key === 'Enter' || e.key === 'c' || e.key === 'C') {
      e.preventDefault();
      if (name.length === 0 || profile.length === 0) return;
      setAnchor({
        kind: 'node',
        label: name,
        source: `Deployment · ${profile}`,
        jsonPath: deploymentAnchor(profile, name),
      });
    }
  };
  return {
    role: 'button',
    tabIndex: 0,
    'aria-label': enabled ? `${name}. Press C to comment.` : name,
    onKeyDown,
  };
}

/**
 * The four border handles every deployment element carries, each usable as both
 * an edge source and an edge target.
 *
 * A deployment graph has no single flow direction — a browser reaches right into
 * a gateway, a server reaches down into its database, a static-asset server
 * reaches back left to the browser it delivers to. Rather than guess, each box
 * exposes all four sides and the LAYOUT picks the pair, having already computed
 * where both elements sit (see pickHandles in DeploymentFlow). The handles are
 * invisible: they are attachment geometry, not a connection affordance — this
 * graph is read-only.
 */
function EdgeHandles(): ReactNode {
  const hidden = { opacity: 0, width: 1, height: 1, minWidth: 1, minHeight: 1, border: 'none' };
  return (
    <>
      {(
        [
          ['top', Position.Top],
          ['bottom', Position.Bottom],
          ['left', Position.Left],
          ['right', Position.Right],
        ] as const
      ).map(([id, position]) => (
        <Box component="span" key={id}>
          <Handle
            id={`s-${id}`}
            isConnectable={false}
            position={position}
            style={hidden}
            type="source"
          />
          <Handle
            id={`t-${id}`}
            isConnectable={false}
            position={position}
            style={hidden}
            type="target"
          />
        </Box>
      ))}
    </>
  );
}

export interface DeployPersonData {
  name: string;
  description: string;
  profile: string;
  [key: string]: unknown;
}

/**
 * A person the environment's frontend surfaces serve. Drawn in the C4 person
 * idiom — a rounded "head and shoulders" card outside the infrastructure — so the
 * diagram answers "who reaches this system?" before it answers "what runs where?".
 */
export function DeployPersonNode({ data, width, height }: NodeProps): ReactNode {
  const t = useTokens();
  const d = data as DeployPersonData;
  const a11y = useDeployNodeA11y(d.name, d.profile);
  return (
    <Box
      {...a11y}
      sx={{
        width,
        height,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 0.5,
        px: 1,
        bgcolor: t.paperAlt,
        border: `1.5px solid ${t.line}`,
        borderRadius: 999,
        cursor: 'pointer',
        outline: 'none',
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
      }}
    >
      <EdgeHandles />
      <Box
        sx={{
          width: 18,
          height: 18,
          borderRadius: '50%',
          border: `1.5px solid ${t.muted}`,
        }}
      />
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 11,
          color: t.ink,
          textAlign: 'center',
        }}
      >
        {d.name}
      </Typography>
      {d.description.length > 0 && (
        <Typography
          sx={{ fontFamily: t.body, fontSize: 9, color: t.muted, textAlign: 'center', ...clamp2 }}
        >
          {d.description}
        </Typography>
      )}
    </Box>
  );
}

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
  /** How the container is consumed — `service` for the back-end default. */
  surface: string;
  profile: string;
  [key: string]: unknown;
}

export interface DeployInfraData {
  name: string;
  technology: string;
  description: string;
  /** What the element does — `other` when unclassified. */
  role: string;
  profile: string;
  [key: string]: unknown;
}

export interface DeployExternalData {
  name: string;
  technology: string;
  description: string;
  /** What the element does — `other` when unclassified. */
  role: string;
  profile: string;
  [key: string]: unknown;
}

/**
 * Human labels for the classifications worth calling out on the face of a box.
 * A back-end `service` and an unclassified `other` get no chip — the chip is for
 * the elements whose ROLE in the picture is the thing to notice: who the users
 * reach, and what stands at the front door.
 */
const CLASSIFICATION_LABEL: Record<string, string> = {
  spa: 'SPA',
  mobile: 'MOBILE APP',
  cli: 'CLI',
  agentHarness: 'AGENT HARNESS',
  gateway: 'GATEWAY',
  identityProvider: 'IDENTITY',
};

/** The small uppercase chip naming a container surface or an element role. */
function ClassificationChip({ value }: { value: string }): ReactNode {
  const t = useTokens();
  const label = CLASSIFICATION_LABEL[value];
  if (label === undefined) return null;
  return (
    <Box
      sx={{
        alignSelf: 'flex-start',
        px: 0.6,
        py: 0.05,
        borderRadius: 1,
        border: `1px solid ${t.accent}`,
      }}
    >
      <Typography
        sx={{
          fontFamily: t.mono,
          fontWeight: 700,
          fontSize: 8,
          letterSpacing: '0.1em',
          color: t.accent,
        }}
      >
        {label}
      </Typography>
    </Box>
  );
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
  const a11y = useDeployNodeA11y(d.label, d.profile);
  return (
    <Box
      {...a11y}
      sx={{
        width,
        height,
        bgcolor: t.paper,
        border: `1.5px dashed ${t.line}`,
        borderRadius: t.radius / 8 + 0.5,
        cursor: 'pointer',
        outline: 'none',
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
      }}
    >
      <EdgeHandles />
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
  const a11y = useDeployNodeA11y(d.name, d.profile);
  return (
    <Box
      {...a11y}
      sx={{
        position: 'relative',
        width,
        height,
        cursor: 'pointer',
        outline: 'none',
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
      }}
      onMouseEnter={() => {
        setOpen(true);
      }}
      onMouseLeave={() => {
        setOpen(false);
      }}
    >
      <EdgeHandles />
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
        <ClassificationChip value={d.surface} />
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
  const a11y = useDeployNodeA11y(d.name, d.profile);
  return (
    <Box
      {...a11y}
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
        outline: 'none',
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
      }}
    >
      <EdgeHandles />
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
      <ClassificationChip value={d.role} />
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
  const a11y = useDeployNodeA11y(d.name, d.profile);
  return (
    <Box
      {...a11y}
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
        outline: 'none',
        '&:focus-visible': { outline: `2px solid ${t.accent}`, outlineOffset: 2 },
      }}
    >
      <EdgeHandles />
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
      <ClassificationChip value={d.role} />
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
