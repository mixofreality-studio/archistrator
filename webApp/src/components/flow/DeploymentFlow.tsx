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
 * infrastructure — the C4 reading order, and the reason the topology's own node
 * order matters: a model that lists the client device first gets the reference
 * layout (person → device → browser → cluster) for free.
 *
 * RELATIONSHIPS are the point of a deployment view, and they arrive already
 * joined: the architect's AUTHORED edges (the person, the browser, the gateway,
 * the identity provider — endpoints no derivation could know) unioned with the
 * ones the SERVER derived from the committed System model. Because the layout
 * knows where every box ended up, it also picks which side of each box an edge
 * leaves and enters, so lines run the short way round instead of all stacking on
 * one face.
 */
import { useMemo, type ReactNode } from 'react';
import { MarkerType, type Edge, type Node } from '@xyflow/react';
import {
  toDeploymentView,
  type DeploymentEdgeView,
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
import {
  DeployGroupNode,
  DeployContainerNode,
  DeployInfraNode,
  DeployExternalNode,
  DeployPersonNode,
} from './DeploymentNodes';

const nodeTypes = {
  deployGroup: DeployGroupNode,
  deployContainer: DeployContainerNode,
  deployInfra: DeployInfraNode,
  deployExternal: DeployExternalNode,
  deployPerson: DeployPersonNode,
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
const PERSON_W = 150;
const PERSON_H = 96;
/** Gap between the person column and the first root node. */
const PERSON_GUTTER = 48;

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
interface Box {
  id: string;
  cx: number;
  cy: number;
}

/**
 * Top-down: emit a parent group node then its wrapped grid + nested child groups.
 * Every rendered node carries the active `profile` so its Comment affordance can
 * anchor into the committed topology by profile + name, and records its absolute
 * centre in `boxes` keyed by ELEMENT key so the edge pass can find it.
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
  boxes: Map<string, Box>
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
      profile,
    },
    draggable: false,
    selectable: true,
    ...(parentId !== undefined ? { parentId, extent: 'parent' as const } : {}),
  });
  if (sized.node.elementKey.length > 0) {
    boxes.set(sized.node.elementKey, {
      id: idPath,
      cx: absX + sized.w / 2,
      cy: absY + sized.h / 2,
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
    switch (item.kind) {
      case 'container':
        out.push({
          ...base,
          type: 'deployContainer',
          data: {
            name: item.view.name,
            technology: item.view.technology,
            description: item.view.description,
            note: item.view.note,
            components: item.view.components,
            surface: item.view.surface,
            profile,
          },
        });
        break;
      case 'infra':
        out.push({
          ...base,
          type: 'deployInfra',
          data: {
            name: item.view.name,
            technology: item.view.technology,
            description: item.view.description,
            role: item.view.role,
            profile,
          },
        });
        break;
      case 'external':
        out.push({
          ...base,
          type: 'deployExternal',
          data: {
            name: item.view.name,
            technology: item.view.technology,
            description: item.view.description,
            role: item.view.role,
            profile,
          },
        });
        break;
    }
    if (item.view.elementKey.length > 0) {
      boxes.set(item.view.elementKey, {
        id,
        cx: absX + relX + item.w / 2,
        cy: absY + relY + item.h / 2,
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
      boxes
    );
    cursorY += child.h + GAP;
  });
}

/**
 * Picks the pair of border handles an edge should use, from where its two boxes
 * actually sit: the dominant axis of the offset between their centres wins, so a
 * line between side-by-side boxes runs left↔right and one between stacked boxes
 * runs top↔bottom. Without this every edge would leave the same face and the
 * diagram would read as a bundle rather than a graph.
 */
function pickHandles(from: Box, to: Box): { sourceHandle: string; targetHandle: string } {
  const dx = to.cx - from.cx;
  const dy = to.cy - from.cy;
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0
      ? { sourceHandle: 's-right', targetHandle: 't-left' }
      : { sourceHandle: 's-left', targetHandle: 't-right' };
  }
  return dy >= 0
    ? { sourceHandle: 's-bottom', targetHandle: 't-top' }
    : { sourceHandle: 's-top', targetHandle: 't-bottom' };
}

/**
 * How long a DERIVED caption may be before it is clipped.
 *
 * An authored label names the deployment-level interaction and is written for
 * this view ("Makes API calls to"). A derived one is the System relationship's
 * label — a component-level operation list like "read row / guarded in-place
 * update (version + applied_mutation dedup)" — which is both the wrong altitude
 * and long enough to lie across the boxes it passes. Clipped, it still hints at
 * what the edge carries; the authoritative text stays in the committed System
 * model, one click away on the Architecture step.
 */
const DERIVED_LABEL_MAX = 24;

/** The Structurizr-style caption: the label over its `[technology]` line. */
function edgeLabel(edge: DeploymentEdgeView): string {
  const text =
    edge.derived && edge.label.length > DERIVED_LABEL_MAX
      ? `${edge.label.slice(0, DERIVED_LABEL_MAX).trimEnd()}…`
      : edge.label;
  if (edge.technology.length === 0) return text;
  return `${text}\n[${edge.technology}]`;
}

/**
 * Builds the drawn edges from the joined relationship set, dropping any whose
 * endpoints did not make it onto the canvas. A dangling endpoint is the server
 * gate's finding (DEP-EDGE-REF), not something to render as a stray line.
 */
function buildEdges(edges: DeploymentEdgeView[], boxes: Map<string, Box>, t: Tokens): Edge[] {
  const out: Edge[] = [];
  for (const edge of edges) {
    const from = boxes.get(edge.from);
    const to = boxes.get(edge.to);
    if (from === undefined || to === undefined) continue;
    out.push({
      id: edge.id,
      source: from.id,
      target: to.id,
      ...pickHandles(from, to),
      type: 'smoothstep',
      label: edgeLabel(edge),
      labelShowBg: true,
      labelBgPadding: [6, 3],
      labelBgBorderRadius: 3,
      labelBgStyle: { fill: t.paper, fillOpacity: 1 },
      labelStyle: { fontFamily: t.mono, fontSize: 9.5, fill: t.muted },
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: t.muted },
      style: { stroke: t.muted, strokeWidth: 1.4, strokeDasharray: '5 4' },
      zIndex: 1000,
    });
  }
  return out;
}

/** Lays the person column left of the roots, then the roots left to right. */
function build(
  roots: DeploymentNodeView[],
  persons: { elementKey: string; name: string; description: string }[],
  profile: string
): { nodes: Node[]; boxes: Map<string, Box> } {
  const out: Node[] = [];
  const boxes = new Map<string, Box>();

  const personColumnW = persons.length > 0 ? PERSON_W + PERSON_GUTTER : 0;
  persons.forEach((person, i) => {
    const y = i * (PERSON_H + GAP);
    const id = `person-${String(i)}`;
    out.push({
      id,
      type: 'deployPerson',
      position: { x: 0, y },
      width: PERSON_W,
      height: PERSON_H,
      data: { name: person.name, description: person.description, profile },
      draggable: false,
      selectable: true,
    });
    boxes.set(person.elementKey, { id, cx: PERSON_W / 2, cy: y + PERSON_H / 2 });
  });

  let x = personColumnW;
  roots.forEach((root, i) => {
    const sized = measure(root);
    emit(sized, undefined, `root-${String(i)}`, x, 0, x, 0, profile, out, boxes);
    x += sized.w + GAP * 2;
  });
  return { nodes: out, boxes };
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
  const { nodes, edges } = useMemo(() => {
    if (env === undefined) return { nodes: [], edges: [] };
    const built = build(env.roots, env.persons, profile);
    return { nodes: built.nodes, edges: buildEdges(env.edges, built.boxes, t) };
  }, [env, profile, t]);

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
        const d = node.data as { profile?: string; name?: string; label?: string };
        const name = d.name ?? d.label ?? '';
        if (name.length === 0 || d.profile === undefined) return;
        setAnchor({
          kind: 'node',
          label: name,
          source: `Deployment · ${d.profile}`,
          jsonPath: deploymentAnchor(d.profile, name),
        });
      }}
    />
  );
}
