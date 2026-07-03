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
 */
import { useMemo, type ReactNode } from 'react';
import type { Node } from '@xyflow/react';
import {
  toDeploymentView,
  type DeploymentNodeView,
  type ContainerInstanceView,
  type InfraView,
  type ExternalView,
} from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import type { DeploymentProfile } from '../../api/models';
import { useTokens } from '../../theme/ThemeContext';
import { FlowCanvas, FlowEmpty } from './flowShared';
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

/** Top-down: emit a parent group node then its wrapped grid + nested child groups. */
function emit(
  sized: Sized,
  parentId: string | undefined,
  idPath: string,
  x: number,
  y: number,
  out: Node[]
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
    },
    draggable: false,
    selectable: false,
    ...(parentId !== undefined ? { parentId, extent: 'parent' as const } : {}),
  });

  sized.items.forEach((item, i) => {
    const position = { x: PAD + item.x, y: sized.headerH + PAD + item.y };
    const base = {
      id: `${idPath}/item-${String(i)}`,
      position,
      width: item.w,
      height: item.h,
      parentId: idPath,
      extent: 'parent' as const,
      draggable: false,
      selectable: false,
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
          },
        });
        break;
    }
  });

  let cursorY = sized.headerH + PAD + sized.gridH;
  if (sized.gridH > 0 && sized.children.length > 0) cursorY += GAP;

  sized.children.forEach((child, i) => {
    emit(child, idPath, `${idPath}/g-${String(i)}`, PAD, cursorY, out);
    cursorY += child.h + GAP;
  });
}

function build(roots: DeploymentNodeView[]): Node[] {
  const out: Node[] = [];
  let x = 0;
  roots.forEach((root, i) => {
    const sized = measure(root);
    emit(sized, undefined, `root-${String(i)}`, x, 0, out);
    x += sized.w + GAP * 2;
  });
  return out;
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
  const roots = useMemo(
    () => toDeploymentView(opEnvelope, systemEnvelope, profile),
    [opEnvelope, systemEnvelope, profile]
  );
  const nodes = useMemo(() => (roots !== undefined ? build(roots) : []), [roots]);

  if (roots === undefined || nodes.length === 0) {
    return <FlowEmpty label="No deployment topology for this profile." t={t} />;
  }

  return <FlowCanvas edges={[]} height={height} nodeTypes={nodeTypes} nodes={nodes} t={t} />;
}
