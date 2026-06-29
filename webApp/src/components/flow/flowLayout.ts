/**
 * Pure (non-component) layout primitives for the architecture flow family: the
 * layer vocabulary, colour map, C4-node + edge factories, and the React-Flow
 * node-type registry. Kept JSX-free so it can be shared without tripping the
 * react-refresh "only export components" rule (the JSX chrome lives in
 * ./flowShared).
 */
import { MarkerType, type Edge, type Node } from '@xyflow/react';
import type { Layer } from '../../api/models';
import type { Tokens } from '../../theme/themes';
import type { C4Component } from '../../api/adapters';
import { C4Node } from './C4Node';

export type { Layer };

/** React-Flow node-type registry shared by every C4-style flow in the family. */
export const nodeTypes = { c4: C4Node };

/** Method layer stack, top-to-bottom (Resources/Utility share the bottom band). */
export const LAYER_ORDER: readonly Layer[] = [
  'client',
  'manager',
  'engine',
  'resourceAccess',
  'resource',
  'utility',
];

export const LAYER_LABEL: Record<Layer, string> = {
  client: 'Clients',
  manager: 'Managers',
  engine: 'Engines',
  resourceAccess: 'ResourceAccess',
  resource: 'Resources',
  utility: 'Utility',
};

export function layerColors(t: Tokens): Record<Layer, string> {
  return {
    client: t.accent,
    manager: t.accent2,
    engine: t.committedDot,
    resourceAccess: t.awaitingFg,
    resource: t.muted,
    utility: t.muted,
  };
}

export const COL_W = 220;
export const ROW_H = 150;

/** Builds a `c4`-type React-Flow node for one component at an explicit position. */
export function c4Node(
  c: C4Component,
  position: { x: number; y: number },
  colors: Record<Layer, string>
): Node {
  return {
    id: c.id,
    type: 'c4',
    position,
    data: {
      componentId: c.id,
      name: c.name,
      layer: LAYER_LABEL[c.layer],
      encapsulates: c.encapsulates,
      color: colors[c.layer],
    },
    draggable: false,
  };
}

/** A labelled, arrow-headed smoothstep edge in the shared visual language. */
export function flowEdge(
  id: string,
  source: string,
  target: string,
  label: string,
  t: Tokens
): Edge {
  return {
    id,
    source,
    target,
    label,
    type: 'smoothstep',
    style: { stroke: t.muted, strokeWidth: 1.5 },
    labelStyle: { fontFamily: t.mono, fontSize: 10, fontWeight: 700, fill: t.ink },
    labelBgStyle: { fill: t.paper, fillOpacity: 0.95 },
    labelBgPadding: [5, 3] as [number, number],
    markerEnd: { type: MarkerType.ArrowClosed, color: t.muted },
  };
}
