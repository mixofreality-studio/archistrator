/**
 * The GRAPH lens's React-Flow node-type registry. Its own module, importing the
 * node components, so the component modules export only components (the
 * react-refresh rule — the same split flow/flowNodeTypes.ts makes).
 *
 * The row-label gutter and the Utilities frame are the house decor node types,
 * reused unchanged from the architecture diagrams (founder convention: layered
 * top-down, utilities in a boxed side bar with no lines).
 */
import type { NodeTypes } from '@xyflow/react';
import { RowLabelNode, UtilityFrameNode } from '../../flow/flowDecor';
import { GraphCardNode } from './GraphNodes';

export const graphNodeTypes: NodeTypes = {
  graphCard: GraphCardNode,
  rowLabel: RowLabelNode,
  utilityFrame: UtilityFrameNode,
};
