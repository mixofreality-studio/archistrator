/**
 * The deployment topology for ONE profile, rendered with @xyflow/react parent/
 * child (group) nesting: each nested DeploymentNode (cluster → namespace) becomes
 * a `deployGroup` parent node, and each ContainerInstance becomes a `deployInstance`
 * child node (parentId + extent:'parent') coloured by its System component's Method
 * layer. Inside a container, instances are bucketed into Method-layer rows and
 * stacked in the same layered order as the static architecture diagram (Clients →
 * Managers → Engines → ResourceAccess → Resources → Utility), each row tagged with
 * a layer label in the left gutter. A bottom-up pass sizes every group to fit its
 * rows + nested children, then a top-down pass places them; fit-to-view in canvas.
 */
import { useMemo, type ReactNode } from 'react';
import type { Node } from '@xyflow/react';
import {
  toDeploymentView,
  type DeploymentNodeView,
  type DeploymentInstance,
} from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import type { Layer, DeploymentProfile } from '../../api/models';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { layerColors, LAYER_LABEL, LAYER_ORDER } from './flowLayout';
import { FlowCanvas, FlowEmpty } from './flowShared';
import { DeployGroupNode, DeployInstanceNode, DeployLayerLabelNode } from './DeploymentNodes';

const nodeTypes = {
  deployGroup: DeployGroupNode,
  deployLayerLabel: DeployLayerLabelNode,
  deployInstance: DeployInstanceNode,
};

const HEADER_H = 38; // group header band
const PAD = 14; // inner padding
const GAP = 14; // gap between siblings
const ROW_GAP = 10; // gap between layer rows within a container
const GUTTER = 104; // left gutter holding the layer label
const INST_W = 168;
const INST_H = 92;

interface LayerRow {
  layer: Layer;
  insts: DeploymentInstance[];
}

interface Sized {
  node: DeploymentNodeView;
  w: number;
  h: number;
  rows: LayerRow[];
  children: Sized[];
}

/** Bottom-up: measure each group big enough to hold its layer rows + child groups. */
function measure(node: DeploymentNodeView): Sized {
  const children = node.children.map(measure);

  // Instances bucketed into Method-layer rows, ordered like the static diagram.
  const rows: LayerRow[] = LAYER_ORDER.map((layer) => ({
    layer,
    insts: node.instances.filter((inst) => inst.layer === layer),
  })).filter((r) => r.insts.length > 0);

  const rowsW = rows.reduce(
    (m, r) => Math.max(m, GUTTER + r.insts.length * INST_W + (r.insts.length - 1) * GAP),
    0
  );
  const rowsH = rows.length > 0 ? rows.length * INST_H + (rows.length - 1) * ROW_GAP : 0;

  // Child groups stacked vertically.
  const childMaxW = children.reduce((m, c) => Math.max(m, c.w), 0);
  const childStackH =
    children.reduce((sum, c) => sum + c.h, 0) + Math.max(children.length - 1, 0) * GAP;

  const innerW = Math.max(rowsW, childMaxW, GUTTER + INST_W);
  const innerH = rowsH + (rows.length > 0 && children.length > 0 ? GAP : 0) + childStackH;

  return {
    node,
    w: innerW + PAD * 2,
    h: HEADER_H + innerH + PAD,
    rows,
    children,
  };
}

/** Top-down: emit a parent group node then its layer rows + nested child groups. */
function emit(
  sized: Sized,
  parentId: string | undefined,
  idPath: string,
  x: number,
  y: number,
  t: Tokens,
  colors: Record<Layer, string>,
  out: Node[]
): void {
  out.push({
    id: idPath,
    type: 'deployGroup',
    position: { x, y },
    width: sized.w,
    height: sized.h,
    data: { label: sized.node.name, technology: sized.node.technology },
    draggable: false,
    selectable: false,
    ...(parentId !== undefined ? { parentId, extent: 'parent' as const } : {}),
  });

  let cursorY = HEADER_H + PAD;

  sized.rows.forEach((row, ri) => {
    out.push({
      id: `${idPath}/label-${row.layer}`,
      type: 'deployLayerLabel',
      position: { x: PAD, y: cursorY },
      width: GUTTER,
      height: INST_H,
      data: { label: LAYER_LABEL[row.layer], color: colors[row.layer] },
      parentId: idPath,
      extent: 'parent' as const,
      draggable: false,
      selectable: false,
    });
    row.insts.forEach((inst, i) => {
      out.push({
        id: `${idPath}/inst-${row.layer}-${String(i)}`,
        type: 'deployInstance',
        position: { x: PAD + GUTTER + i * (INST_W + GAP), y: cursorY },
        width: INST_W,
        height: INST_H,
        data: {
          name: inst.name,
          layerLabel: LAYER_LABEL[inst.layer],
          color: colors[inst.layer],
          note: inst.note,
        },
        parentId: idPath,
        extent: 'parent' as const,
        draggable: false,
        selectable: false,
      });
    });
    cursorY += INST_H + (ri < sized.rows.length - 1 ? ROW_GAP : 0);
  });

  if (sized.rows.length > 0 && sized.children.length > 0) cursorY += GAP;

  sized.children.forEach((child, i) => {
    emit(child, idPath, `${idPath}/g-${String(i)}`, PAD, cursorY, t, colors, out);
    cursorY += child.h + GAP;
  });
}

function build(roots: DeploymentNodeView[], t: Tokens): Node[] {
  const colors = layerColors(t);
  const out: Node[] = [];
  let x = 0;
  roots.forEach((root, i) => {
    const sized = measure(root);
    emit(sized, undefined, `root-${String(i)}`, x, 0, t, colors, out);
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
  const nodes = useMemo(() => (roots !== undefined ? build(roots, t) : []), [roots, t]);

  if (roots === undefined || nodes.length === 0) {
    return <FlowEmpty label="No deployment topology for this profile." t={t} />;
  }

  return <FlowCanvas edges={[]} height={height} nodeTypes={nodeTypes} nodes={nodes} t={t} />;
}
