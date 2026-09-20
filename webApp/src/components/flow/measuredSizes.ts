/**
 * Every node's MEASURED size, as one key — the dependency a self-sizing canvas
 * was missing.
 *
 * A drawing does not only change when its NODES change: it RE-MEASURES. A
 * webfont swapping in after the first paint is the everyday case — the op
 * signatures are drawn in the fallback, wrap at the interface node's fixed
 * width, and unwrap a moment later when the real face arrives, leaving the node
 * shorter than it was when the canvas was fitted to it. Fitted once and never
 * again, the frame keeps the taller height and an empty band opens below the
 * drawing: exactly what "a canvas its own size" (fitContent.ts) promises not to
 * do. It went red on CI's Linux runner and stayed green on macOS, where the same
 * fonts happen to be warm before the first measure (uitests S4).
 *
 * The sizes here are ResizeObserver layout boxes, so they do not move with the
 * viewport's zoom — feeding them to a fit's deps re-fits on a real re-measure
 * and cannot chase its own transform.
 *
 * Its own module, not a second export beside the flow components: a file that
 * exports both components and helpers loses Fast Refresh (react-refresh lint).
 */
import { useStore } from '@xyflow/react';

export function useMeasuredSizes(): string {
  return useStore((s) => {
    let key = '';
    for (const node of s.nodeLookup.values()) {
      key += `${node.id}:${String(node.measured.width ?? 0)}x${String(node.measured.height ?? 0)};`;
    }
    return key;
  });
}
