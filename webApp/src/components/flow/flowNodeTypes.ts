/**
 * The React-Flow node-type registry shared by every C4-style flow in the family.
 * Kept in its own module (importing the node components) so the component modules
 * stay free of non-component exports — react-refresh requires that split.
 */
import type { EdgeTypes, NodeTypes } from '@xyflow/react';
import { C4Node } from './C4Node';
import { LayeredStepEdge, RowLabelNode, UtilityFrameNode } from './flowDecor';

export const nodeTypes: NodeTypes = {
  c4: C4Node,
  rowLabel: RowLabelNode,
  utilityFrame: UtilityFrameNode,
};

export const edgeTypes: EdgeTypes = {
  layeredStep: LayeredStepEdge,
};
