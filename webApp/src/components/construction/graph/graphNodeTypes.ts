/**
 * The GRAPH lens's React-Flow node-type registry. Its own module, importing the
 * node components, so the component modules export only components (the
 * react-refresh rule — the same split flow/flowNodeTypes.ts makes).
 *
 * The Utilities frame is the house decor node, reused unchanged from the
 * architecture diagrams (founder convention: layered top-down, utilities in a
 * boxed side bar with no lines). The row labels are NOT nodes any more: they
 * live in a pinned HTML gutter that never scales (GraphRowGutter, designer
 * P1-6); an invisible `rowSpacer` keeps their room at fit.
 */
import type { NodeTypes } from '@xyflow/react';
import { UtilityFrameNode } from '../../flow/flowDecor';
import { GraphCardNode } from './GraphNodes';
import { RowSpacerNode } from './GraphRowGutter';

export const graphNodeTypes: NodeTypes = {
  graphCard: GraphCardNode,
  rowSpacer: RowSpacerNode,
  utilityFrame: UtilityFrameNode,
};
