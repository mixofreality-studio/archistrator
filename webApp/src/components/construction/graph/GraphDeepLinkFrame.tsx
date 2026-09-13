/**
 * Frames a deep-linked card ONCE, at mount (graphViewport.graphMountFor), in the
 * part of the canvas a reader can see: below 1200px the graph's non-modal detail
 * drawer overlays the canvas's right side, and centring in the whole canvas put
 * the card under the drawer (designer re-check 4). The drawer's width is read at
 * framing time, in an effect — never during render — and the card is centred in
 * the visible width (visibleCanvasWidthPx). With no drawer it frames exactly as
 * the shared FocusNodes did: padding 0.4, never past zoom 1.2.
 */
import { useEffect } from 'react';
import { getViewportForBounds, useReactFlow, useStoreApi } from '@xyflow/react';

import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { prefersReducedMotion } from '../../../utilities/reducedMotion';
import { visibleCanvasWidthPx } from './graphViewport';

const FRAME_PADDING = 0.4;
const FRAME_MAX_ZOOM = 1.2;
const CANVAS_SELECTOR = `[data-testid="${UI_IDENTIFIERS.Construction.GRAPH_CANVAS}"]`;
const DRAWER_PAPER_SELECTOR = `[data-testid="${UI_IDENTIFIERS.Construction.DETAIL_DRAWER}"] [role="dialog"]`;

export function GraphDeepLinkFrame({ cardId }: { cardId: string }): null {
  const { getNodesBounds, setViewport } = useReactFlow();
  const store = useStoreApi();
  useEffect(() => {
    let raf2 = 0;
    const raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => {
        const canvas = document.querySelector(CANVAS_SELECTOR)?.getBoundingClientRect();
        if (canvas === undefined) return;
        // The drawer is anchored right; its paper's layout width is its final
        // width even while it slides in.
        const paper = document.querySelector(DRAWER_PAPER_SELECTOR);
        const drawerLeft =
          paper instanceof HTMLElement ? window.innerWidth - paper.offsetWidth : undefined;
        const { height, minZoom } = store.getState();
        const viewport = getViewportForBounds(
          getNodesBounds([cardId]),
          visibleCanvasWidthPx(canvas.left, canvas.right, drawerLeft),
          height,
          minZoom,
          FRAME_MAX_ZOOM,
          FRAME_PADDING
        );
        void setViewport(viewport, { duration: prefersReducedMotion() ? 0 : 400 });
      });
    });
    return (): void => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
  }, [cardId, getNodesBounds, setViewport, store]);
  return null;
}
