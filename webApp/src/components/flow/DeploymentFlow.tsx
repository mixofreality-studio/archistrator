/**
 * The deployment topology for ONE profile, rendered with @xyflow/react parent/
 * child (group) nesting: each nested DeploymentNode (cluster → namespace) becomes
 * a `deployGroup` parent node holding a small left-to-right wrapped grid of the
 * container instances / infrastructure / external systems it hosts — each its own
 * node type (`deployContainer` / `deployInfra` / `deployExternal`), the true C4
 * "primary unit" (packaged System components are a secondary, expandable list
 * inside the container box, not separate boxes). Nested child DeploymentNodes are
 * stacked below that grid. A bottom-up pass sizes every group to fit its wrapped
 * grid + nested children, then a top-down pass places them; fit-to-view in canvas.
 *
 * The people the environment serves are drawn in a column to the LEFT of the
 * infrastructure, using the SAME `person` node the dynamic call-chain view uses —
 * the UML actor glyph — so an actor reads identically wherever it appears.
 *
 * RELATIONSHIPS are the point of a deployment view, and they arrive already
 * joined: the architect's AUTHORED edges (the person, the browser, the gateway,
 * the identity provider — endpoints no derivation could know) unioned with the
 * ones the SERVER derived from the committed System model.
 *
 * INTERACTION follows the static architecture view exactly, and for the same
 * reason. Every edge labelled at once collapses into an unreadable band lying
 * across the boxes it passes — so nothing is labelled by default and the lines
 * stay quiet. Hover (or click to pin) an element and its neighbourhood stays lit
 * while the rest fades, with the incident edges highlighted AND labelled. The
 * labels exist for the one relationship you are looking at, not for all of them.
 *
 * Edges are built by the shared `flowEdge` factory, so stroke, arrowhead, label
 * styling and the focus/muted variants are the same values every other flow in
 * the family uses; only the handle PAIR is chosen here, because a deployment
 * graph has no single flow direction and the layout knows where each box landed.
 */
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Panel, type Edge, type Node } from '@xyflow/react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import {
  toDeploymentView,
  type DeploymentEdgeView,
  type DeploymentEnvironmentView,
  type DeploymentNodeView,
  type ContainerInstanceView,
  type InfraView,
  type ExternalView,
} from '../../contracts/adapters';
import type { ArtifactModelEnvelope, DeploymentProfile } from '../../contracts/types';
import { useTokens } from '../../utilities/theme/ThemeContext';
import type { Tokens } from '../../utilities/theme/themes';
import { useComments, deploymentAnchor } from '../comments/CommentContext';
import { FlowCanvas, FlowEmpty } from './flowShared';
import { flowEdge, layerColors, MUTED_OPACITY, NODE_W } from './flowLayout';
import { PersonNode } from './PersonNode';
import {
  DeployGroupNode,
  DeployContainerNode,
  DeployInfraNode,
  DeployExternalNode,
} from './DeploymentNodes';

const nodeTypes = {
  deployGroup: DeployGroupNode,
  deployContainer: DeployContainerNode,
  deployInfra: DeployInfraNode,
  deployExternal: DeployExternalNode,
  person: PersonNode,
};

const HEADER_H = 38; // group header band (label + technology)
const DESC_H = 22; // extra header height when the group carries a description
const PAD = 14; // inner padding
const GAP = 14; // gap between siblings / grid cells
const MIN_INNER_W = 220; // floor so an empty/near-empty group still reads as a box

const CONTAINER_W = 208;
const CONTAINER_H = 132;
const INFRA_W = 176;
const INFRA_H = 84;
const EXTERNAL_W = 176;
const EXTERNAL_H = 84;
/** The shared person node sizes itself to NODE_W; this is its measured height. */
const PERSON_H = 74;
/** Gap between the person column and the first root node. */
const PERSON_GUTTER = 56;

/** Wrap a row of boxes after roughly this many px before starting a new one. */
const MAX_ROW_W = 3 * CONTAINER_W + 2 * GAP;

interface ContainerItem {
  kind: 'container';
  w: number;
  h: number;
  view: ContainerInstanceView;
}
interface InfraItem {
  kind: 'infra';
  w: number;
  h: number;
  view: InfraView;
}
interface ExternalItem {
  kind: 'external';
  w: number;
  h: number;
  view: ExternalView;
}
type GridItem = ContainerItem | InfraItem | ExternalItem;
type PlacedItem = GridItem & { x: number; y: number };

/** Header height for a group, given whether it carries a description line. */
function groupHeaderH(node: DeploymentNodeView): number {
  return HEADER_H + (node.description.length > 0 ? DESC_H : 0);
}

/** The container/infra/external boxes a deployment node hosts, in C4 reading order. */
function buildItems(node: DeploymentNodeView): GridItem[] {
  const containers: ContainerItem[] = node.containers.map((view) => ({
    kind: 'container',
    w: CONTAINER_W,
    h: CONTAINER_H,
    view,
  }));
  const infra: InfraItem[] = node.infrastructure.map((view) => ({
    kind: 'infra',
    w: INFRA_W,
    h: INFRA_H,
    view,
  }));
  const externals: ExternalItem[] = node.externals.map((view) => ({
    kind: 'external',
    w: EXTERNAL_W,
    h: EXTERNAL_H,
    view,
  }));
  return [...containers, ...infra, ...externals];
}

/** Left-to-right wrap: place items in rows, wrapping once a row exceeds MAX_ROW_W. */
function wrapGrid(items: GridItem[]): { placed: PlacedItem[]; w: number; h: number } {
  let cursorX = 0;
  let cursorY = 0;
  let rowH = 0;
  let maxX = 0;
  const placed: PlacedItem[] = [];
  items.forEach((item) => {
    if (cursorX > 0 && cursorX + item.w > MAX_ROW_W) {
      cursorY += rowH + GAP;
      cursorX = 0;
      rowH = 0;
    }
    placed.push({ ...item, x: cursorX, y: cursorY });
    cursorX += item.w + GAP;
    rowH = Math.max(rowH, item.h);
    maxX = Math.max(maxX, cursorX - GAP);
  });
  return { placed, w: maxX, h: items.length > 0 ? cursorY + rowH : 0 };
}

interface Sized {
  node: DeploymentNodeView;
  w: number;
  h: number;
  headerH: number;
  items: PlacedItem[];
  gridH: number;
  children: Sized[];
}

/** Bottom-up: measure each group big enough to hold its wrapped grid + child groups. */
function measure(node: DeploymentNodeView): Sized {
  const children = node.children.map(measure);
  const grid = wrapGrid(buildItems(node));
  const headerH = groupHeaderH(node);

  const childMaxW = children.reduce((m, c) => Math.max(m, c.w), 0);
  const childStackH =
    children.reduce((sum, c) => sum + c.h, 0) + Math.max(children.length - 1, 0) * GAP;

  const innerW = Math.max(grid.w, childMaxW, MIN_INNER_W);
  const innerH = grid.h + (grid.h > 0 && childStackH > 0 ? GAP : 0) + childStackH;

  return {
    node,
    w: innerW + PAD * 2,
    h: headerH + innerH + PAD,
    headerH,
    items: grid.placed,
    gridH: grid.h,
    children,
  };
}

/** Where an element ended up on the canvas, in ABSOLUTE coordinates. Edges are
 *  routed from this, which is why the layout — not the edge renderer — decides
 *  which face of a box a line attaches to. */
interface Placed {
  /** The React-Flow node id (distinct from the element key the model uses). */
  id: string;
  cx: number;
  cy: number;
  /** True for the shared `person` node, whose handle set is fixed. */
  person: boolean;
}

/** The built canvas: the nodes plus where each ELEMENT key landed. */
interface Built {
  nodes: Node[];
  placed: Map<string, Placed>;
}

/**
 * Top-down: emit a parent group node then its wrapped grid + nested child groups.
 * Every rendered node carries the active `profile` so its Comment affordance can
 * anchor into the committed topology by profile + name, its element key so hover
 * can resolve the neighbourhood, and its absolute centre so the edge pass can
 * pick handles.
 *
 * React Flow positions a child relative to its parent, so `position` stays
 * relative while `absX`/`absY` accumulate down the tree.
 */
function emit(
  sized: Sized,
  parentId: string | undefined,
  idPath: string,
  x: number,
  y: number,
  absX: number,
  absY: number,
  profile: string,
  out: Node[],
  placed: Map<string, Placed>
): void {
  out.push({
    id: idPath,
    type: 'deployGroup',
    position: { x, y },
    width: sized.w,
    height: sized.h,
    data: {
      label: sized.node.name,
      technology: sized.node.technology,
      description: sized.node.description,
      instances: sized.node.instances,
      elementKey: sized.node.elementKey,
      profile,
    },
    draggable: false,
    selectable: true,
    ...(parentId !== undefined ? { parentId, extent: 'parent' as const } : {}),
  });
  if (sized.node.elementKey.length > 0) {
    placed.set(sized.node.elementKey, {
      id: idPath,
      cx: absX + sized.w / 2,
      cy: absY + sized.h / 2,
      person: false,
    });
  }

  sized.items.forEach((item, i) => {
    const relX = PAD + item.x;
    const relY = sized.headerH + PAD + item.y;
    const id = `${idPath}/item-${String(i)}`;
    const base = {
      id,
      position: { x: relX, y: relY },
      width: item.w,
      height: item.h,
      parentId: idPath,
      extent: 'parent' as const,
      draggable: false,
      selectable: true,
    };
    const shared = { elementKey: item.view.elementKey, profile };
    switch (item.kind) {
      case 'container':
        out.push({
          ...base,
          type: 'deployContainer',
          data: {
            ...shared,
            name: item.view.name,
            technology: item.view.technology,
            description: item.view.description,
            note: item.view.note,
            components: item.view.components,
            surface: item.view.surface,
          },
        });
        break;
      case 'infra':
        out.push({
          ...base,
          type: 'deployInfra',
          data: {
            ...shared,
            name: item.view.name,
            technology: item.view.technology,
            description: item.view.description,
            role: item.view.role,
          },
        });
        break;
      case 'external':
        out.push({
          ...base,
          type: 'deployExternal',
          data: {
            ...shared,
            name: item.view.name,
            technology: item.view.technology,
            description: item.view.description,
            role: item.view.role,
          },
        });
        break;
    }
    if (item.view.elementKey.length > 0) {
      placed.set(item.view.elementKey, {
        id,
        cx: absX + relX + item.w / 2,
        cy: absY + relY + item.h / 2,
        person: false,
      });
    }
  });

  let cursorY = sized.headerH + PAD + sized.gridH;
  if (sized.gridH > 0 && sized.children.length > 0) cursorY += GAP;

  sized.children.forEach((child, i) => {
    emit(
      child,
      idPath,
      `${idPath}/g-${String(i)}`,
      PAD,
      cursorY,
      absX + PAD,
      absY + cursorY,
      profile,
      out,
      placed
    );
    cursorY += child.h + GAP;
  });
}

/** Lays the person column left of the roots, then the roots left to right. */
function build(env: DeploymentEnvironmentView, profile: string, t: Tokens): Built {
  const out: Node[] = [];
  const placed = new Map<string, Placed>();
  const personColor = layerColors(t).person;

  const personColumnW = env.persons.length > 0 ? NODE_W + PERSON_GUTTER : 0;
  env.persons.forEach((person, i) => {
    const y = i * (PERSON_H + GAP);
    const id = `person-${String(i)}`;
    out.push({
      id,
      type: 'person',
      position: { x: 0, y },
      data: {
        personId: person.name,
        role: person.description,
        color: personColor,
        elementKey: person.elementKey,
        profile,
      },
      draggable: false,
      selectable: true,
    });
    placed.set(person.elementKey, { id, cx: NODE_W / 2, cy: y + PERSON_H / 2, person: true });
  });

  let x = personColumnW;
  env.roots.forEach((root, i) => {
    const sized = measure(root);
    emit(sized, undefined, `root-${String(i)}`, x, 0, x, 0, profile, out, placed);
    x += sized.w + GAP * 2;
  });
  return { nodes: out, placed };
}

/**
 * Picks the pair of border handles an edge should use, from where its two boxes
 * actually sit: the dominant axis of the offset between their centres wins, so a
 * line between side-by-side boxes runs left↔right and one between stacked boxes
 * runs top↔bottom. Without this every edge would leave the same face and the
 * diagram would read as a bundle rather than a graph.
 *
 * The shared `person` node is the exception: it carries the dynamic view's fixed
 * handle set rather than the four-sided one, and it always sits left of what it
 * reaches, so it leaves by its right-hand source handle.
 */
function pickHandles(from: Placed, to: Placed): { source: string; target: string } {
  if (from.person) return { source: 'sr', target: 't-left' };
  const dx = to.cx - from.cx;
  const dy = to.cy - from.cy;
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0
      ? { source: 's-right', target: to.person ? 'tl' : 't-left' }
      : { source: 's-left', target: to.person ? 'tl' : 't-right' };
  }
  return dy >= 0
    ? { source: 's-bottom', target: to.person ? 't' : 't-top' }
    : { source: 's-top', target: to.person ? 't' : 't-bottom' };
}

/**
 * How long a DERIVED caption may be before it is clipped.
 *
 * An authored label names the deployment-level interaction and is written for
 * this view ("Makes API calls to"). A derived one is the System relationship's
 * label — a component-level operation list like "read row / guarded in-place
 * update (version + applied_mutation dedup)" — long enough to lie across the
 * boxes it passes even when only one edge is lit. Clipped, it still says what
 * the edge carries; the full text lives on the Architecture step.
 */
const DERIVED_LABEL_MAX = 32;

/** The Structurizr-style caption: the label plus its `[technology]`. */
function edgeLabel(edge: DeploymentEdgeView): string {
  const text =
    edge.derived && edge.label.length > DERIVED_LABEL_MAX
      ? `${edge.label.slice(0, DERIVED_LABEL_MAX).trimEnd()}…`
      : edge.label;
  return edge.technology.length > 0 ? `${text} [${edge.technology}]` : text;
}

/** The element keys directly connected to one element, in either direction. */
function neighbourhood(elementKey: string, edges: DeploymentEdgeView[]): Set<string> {
  const near = new Set<string>([elementKey]);
  for (const edge of edges) {
    if (edge.from === elementKey) near.add(edge.to);
    if (edge.to === elementKey) near.add(edge.from);
  }
  return near;
}

/**
 * Builds the drawn edges from the joined relationship set, dropping any whose
 * endpoints did not make it onto the canvas (a dangling endpoint is the server
 * gate's finding, DEP-EDGE-REF, not a stray line to draw).
 *
 * With nothing focused every edge is quiet and unlabelled. With an element
 * focused only its incident edges remain, highlighted and captioned.
 */
function buildEdges(
  edges: DeploymentEdgeView[],
  placed: Map<string, Placed>,
  activeKey: string | null,
  t: Tokens
): Edge[] {
  const out: Edge[] = [];
  for (const edge of edges) {
    const from = placed.get(edge.from);
    const to = placed.get(edge.to);
    if (from === undefined || to === undefined) continue;
    const incident = activeKey !== null && (edge.from === activeKey || edge.to === activeKey);
    out.push(
      flowEdge(edge.id, from.id, to.id, edgeLabel(edge), t, {
        handles: pickHandles(from, to),
        // A derived edge is dashed: it is not a line anyone drew, it is the
        // architecture showing through.
        dashed: edge.derived,
        ...(activeKey === null
          ? {}
          : { hidden: !incident, variant: incident ? 'focus' : 'muted', showLabel: incident }),
      })
    );
  }
  return out;
}

/**
 * The deployment legend, in the same top-left Panel idiom the layered views use
 * for their layer key: what the two line kinds mean, and the one gesture that
 * reveals a label.
 */
function DeploymentLegend({ t }: { t: Tokens }): ReactNode {
  const rows: { style: 'solid' | 'dashed'; text: string }[] = [
    { style: 'solid', text: 'Authored — the front door' },
    { style: 'dashed', text: 'Derived from the architecture' },
  ];
  return (
    <Panel position="top-left">
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          gap: 0.5,
          p: 1,
          bgcolor: t.paper,
          border: `1.5px solid ${t.line}`,
          borderRadius: t.radius / 8 + 0.5,
        }}
      >
        {rows.map((row) => (
          <Box key={row.text} sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
            <Box sx={{ width: 16, borderTop: `2px ${row.style} ${t.muted}`, flexShrink: 0 }} />
            <Typography
              sx={{ fontFamily: t.mono, fontSize: 10.5, color: t.ink, whiteSpace: 'nowrap' }}
            >
              {row.text}
            </Typography>
          </Box>
        ))}
        <Typography sx={{ fontFamily: t.mono, fontSize: 10, color: t.muted, whiteSpace: 'nowrap' }}>
          Hover an element to light its connections.
        </Typography>
      </Box>
    </Panel>
  );
}

export function DeploymentFlow({
  opEnvelope,
  systemEnvelope,
  profile,
  height = 520,
}: {
  opEnvelope: ArtifactModelEnvelope | undefined;
  systemEnvelope: ArtifactModelEnvelope | undefined;
  profile: DeploymentProfile;
  height?: number;
}): ReactNode {
  const t = useTokens();
  const { setAnchor } = useComments();
  const env = useMemo(
    () => toDeploymentView(opEnvelope, systemEnvelope, profile),
    [opEnvelope, systemEnvelope, profile]
  );

  // Hover wins while active; a click PINS an element so keyboard and touch reach
  // the same neighbourhood highlight that hover gives a mouse. Both are keyed by
  // ELEMENT key, not React-Flow node id, because the relationships are.
  const [hoveredKey, setHoveredKey] = useState<string | null>(null);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const activeKey = hoveredKey ?? selectedKey;

  const built = useMemo(
    () => (env === undefined ? undefined : build(env, profile, t)),
    [env, profile, t]
  );

  const nodes = useMemo(() => {
    if (built === undefined || env === undefined) return [];
    const near = activeKey === null ? null : neighbourhood(activeKey, env.edges);
    return built.nodes.map((node) => {
      const key = (node.data as { elementKey?: string }).elementKey ?? '';
      // Group nodes are PLACES — a namespace does not dim just because the focus
      // sits outside it, or the boxes inside would float without their frame.
      const dimmed = near !== null && node.type !== 'deployGroup' && !near.has(key);
      return dimmed ? { ...node, style: { opacity: MUTED_OPACITY } } : node;
    });
  }, [built, env, activeKey]);

  const edges = useMemo(
    () =>
      built === undefined || env === undefined
        ? []
        : buildEdges(env.edges, built.placed, activeKey, t),
    [built, env, activeKey, t]
  );

  // Hover focus, debounced exactly as the static architecture view does it:
  // crossing the gap between two boxes fires leave-then-enter, and clearing
  // immediately would flash the whole un-muted diagram in between.
  const leaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancelPendingLeave = (): void => {
    if (leaveTimer.current !== null) {
      clearTimeout(leaveTimer.current);
      leaveTimer.current = null;
    }
  };
  const enterNode = useCallback((n: Node): void => {
    cancelPendingLeave();
    if (n.type === 'deployGroup') return; // a place, not a participant
    const key = (n.data as { elementKey?: string }).elementKey ?? '';
    if (key.length > 0) setHoveredKey(key);
  }, []);
  const leaveNode = useCallback((): void => {
    cancelPendingLeave();
    leaveTimer.current = setTimeout(() => {
      setHoveredKey(null);
      leaveTimer.current = null;
    }, 90);
  }, []);
  useEffect(() => cancelPendingLeave, []);

  if (env === undefined || nodes.length === 0) {
    return <FlowEmpty label="No deployment topology for this profile." t={t} />;
  }

  return (
    <FlowCanvas
      edges={edges}
      height={height}
      nodeTypes={nodeTypes}
      nodes={nodes}
      t={t}
      onNodeClick={(_e, node) => {
        const d = node.data as {
          profile?: string;
          name?: string;
          label?: string;
          elementKey?: string;
        };
        const key = d.elementKey ?? '';
        // Click pins the neighbourhood (toggle off on re-click) — the same
        // gesture the static architecture view uses. Commenting stays the
        // explicit act: the node's own Enter/'c' affordance.
        if (key.length > 0 && node.type !== 'deployGroup') {
          setSelectedKey((s) => (s === key ? null : key));
          return;
        }
        const name = d.name ?? d.label ?? '';
        if (name.length === 0 || d.profile === undefined) return;
        setAnchor({
          kind: 'node',
          label: name,
          source: `Deployment · ${d.profile}`,
          jsonPath: deploymentAnchor(d.profile, name),
        });
      }}
      onNodeMouseEnter={(_e, n) => {
        enterNode(n);
      }}
      onNodeMouseLeave={() => {
        leaveNode();
      }}
    >
      <DeploymentLegend t={t} />
    </FlowCanvas>
  );
}
