/**
 * The deployment topology for ONE profile, rendered with @xyflow/react parent/
 * child (group) nesting: each nested DeploymentNode (cluster → namespace) becomes
 * a `deployGroup` parent node, and each ContainerInstance becomes a `deployInstance`
 * child node (parentId + extent:'parent') coloured by its System component's Method
 * layer. A bottom-up pass sizes every group to fit its instances + nested children,
 * then a top-down pass places them; the result is fit-to-view in the shared canvas.
 */
import { useMemo, type ReactNode } from 'react';
import type { Node } from '@xyflow/react';
import { toDeploymentView, type DeploymentNodeView } from '../../api/adapters';
import type { ArtifactModelEnvelope } from '../../api/types';
import type { components } from '../../api/schema';
import { useTokens } from '../../theme/ThemeContext';
import type { Tokens } from '../../theme/themes';
import { layerColors, LAYER_LABEL } from './flowLayout';
import { FlowCanvas, FlowEmpty } from './flowShared';
import { DeployGroupNode, DeployInstanceNode } from './DeploymentNodes';

const nodeTypes = { deployGroup: DeployGroupNode, deployInstance: DeployInstanceNode };

const HEADER_H = 38; // group header band
const PAD = 14; // inner padding
const GAP = 14; // gap between siblings
const INST_W = 168;
const INST_H = 64;

interface Sized {
  node: DeploymentNodeView;
  w: number;
  h: number;
  children: Sized[];
}

/** Bottom-up: measure each group big enough to hold its instances + child groups. */
function measure(node: DeploymentNodeView): Sized {
  const children = node.children.map(measure);

  // Instances laid out in a single horizontal row.
  const instCount = node.instances.length;
  const instRowW = instCount > 0 ? instCount * INST_W + (instCount - 1) * GAP : 0;
  const instRowH = instCount > 0 ? INST_H : 0;

  // Child groups stacked vertically.
  const childMaxW = children.reduce((m, c) => Math.max(m, c.w), 0);
  const childStackH = children.reduce((sum, c) => sum + c.h, 0) + Math.max(children.length - 1, 0) * GAP;

  const innerW = Math.max(instRowW, childMaxW, INST_W);
  const innerH =
    instRowH + (instCount > 0 && children.length > 0 ? GAP : 0) + childStackH;

  return {
    node,
    w: innerW + PAD * 2,
    h: HEADER_H + innerH + PAD,
    children,
  };
}

/** Top-down: emit a parent group node then its instances + nested child groups. */
function emit(
  sized: Sized,
  parentId: string | undefined,
  idPath: string,
  x: number,
  y: number,
  t: Tokens,
  colors: Record<components['schemas']['Layer'], string>,
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

  sized.node.instances.forEach((inst, i) => {
    out.push({
      id: `${idPath}/inst-${String(i)}`,
      type: 'deployInstance',
      position: { x: PAD + i * (INST_W + GAP), y: cursorY },
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
  if (sized.node.instances.length > 0) cursorY += INST_H + GAP;

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
  profile: components['schemas']['DeploymentProfile'];
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
