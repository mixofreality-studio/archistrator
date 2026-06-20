/**
 * Per-kind on-canvas dimensions for activity-diagram nodes. Shared by ActivityNode
 * (renders each shape at these sizes) and ActivityFlow (centers each node in its
 * swim-lane row using the same measurements).
 */
import type { components } from '../../api/schema';

type ActivityNodeKind = components['schemas']['ActivityNodeKind'];

export const NODE_DIMS: Record<ActivityNodeKind, { w: number; h: number }> = {
  start: { w: 26, h: 26 },
  end: { w: 30, h: 30 },
  action: { w: 200, h: 60 },
  loop: { w: 200, h: 60 },
  goto: { w: 200, h: 60 },
  interruptEdge: { w: 200, h: 60 },
  decision: { w: 132, h: 132 },
  switch: { w: 132, h: 132 },
  merge: { w: 96, h: 96 },
  fork: { w: 176, h: 14 },
  join: { w: 176, h: 14 },
  note: { w: 200, h: 72 },
  swimLane: { w: 200, h: 60 },
};
