/**
 * The GRAPH lens's pinned row-label gutter (designer P1-6) and the invisible
 * spacer nodes that keep its room at fit. The band arithmetic is pure and
 * pinned in rowGutter.ts; this only paints it.
 *
 * Rendered as a child of <ReactFlow> (FlowCanvas's children), so it reads the
 * live viewport transform from xyflow's store and stays in step with every pan
 * and zoom, while sitting OVER the canvas as HTML — its labels never scale.
 */
import type { ReactElement } from 'react';
import { useStore } from '@xyflow/react';
import Box from '@mui/material/Box';
import { alpha } from '@mui/material/styles';

import { useTokens } from '../../../utilities/theme/ThemeContext';
import { UI_IDENTIFIERS } from '../../../utilities/constants/UIIdentifiers';
import { GUTTER_W, NODE_H } from '../../flow/flowLayout';
import type { GraphLayoutRow } from './activityGraphLayout';
import {
  GUTTER_PX,
  GUTTER_SOLID_PX,
  gutterLabelFontPx,
  gutterWidthFor,
  rowGutterLabels,
} from './rowGutter';

export function GraphRowGutter({ rows }: { rows: readonly GraphLayoutRow[] }): ReactElement {
  const t = useTokens();
  const transform = useStore((s) => s.transform);
  const canvasHeight = useStore((s) => s.height);
  const labels = rowGutterLabels(rows, transform, canvasHeight);
  const fontPx = gutterLabelFontPx(transform[2]);
  // Full while the first card column is clear of it; a narrow rail once cards
  // slide beneath (rowGutter.gutterWidthFor, designer re-check 1).
  const width = gutterWidthFor(transform[0]);
  const rail = width < GUTTER_PX;

  return (
    <Box
      data-gutter-mode={rail ? 'rail' : 'full'}
      data-testid={UI_IDENTIFIERS.Construction.GRAPH_ROW_GUTTER}
      sx={{
        position: 'absolute',
        left: 0,
        top: 0,
        bottom: 0,
        width,
        // Under xyflow's panels (the zoom controls), over the cards.
        zIndex: 4,
        pointerEvents: 'none',
        overflow: 'hidden',
        // The canvas ground, fading out — a card slid under the gutter while
        // zoomed in stays faintly visible rather than cut off.
        // A rail is solid — it covers only its own 14px, and its label sits on it.
        background: rail
          ? alpha(t.bg, 0.95)
          : `linear-gradient(to right, ${alpha(t.bg, 0.95)} ${String(GUTTER_SOLID_PX)}px, ${alpha(t.bg, 0)})`,
      }}
    >
      {labels
        .filter((l) => l.visible)
        .map((l) => (
          <Box
            data-testid={UI_IDENTIFIERS.Construction.graphRowLabel(l.row)}
            key={l.row}
            sx={{
              position: 'absolute',
              left: 0,
              width: rail ? width : GUTTER_PX - 8,
              top: l.top,
              height: l.height,
              display: 'flex',
              alignItems: 'center',
              justifyContent: rail ? 'center' : 'flex-end',
            }}
          >
            <Box
              component="span"
              sx={{
                fontFamily: t.mono,
                fontWeight: 700,
                fontSize: fontPx,
                letterSpacing: '0.06em',
                textTransform: 'uppercase',
                color: t.muted,
                textAlign: 'right',
                whiteSpace: rail ? 'nowrap' : 'pre-line',
                lineHeight: 1.15,
                // On the rail the label runs bottom-to-top, one line.
                ...(rail ? { writingMode: 'vertical-rl', transform: 'rotate(180deg)' } : {}),
              }}
            >
              {rail ? l.label.replace('\n', ' ') : l.label}
            </Box>
          </Box>
        ))}
    </Box>
  );
}

/** The spacer node's width — the lens hands it to xyflow too, so the node is
 *  never unmeasured. */
export const ROW_SPACER_W = GUTTER_W - 22;

/** Where a row label node used to be: nothing drawn, but fitView still counts
 *  it, so the fitted canvas keeps the gutter's room clear of cards. */
export function RowSpacerNode(): ReactElement {
  return <Box sx={{ width: ROW_SPACER_W, height: NODE_H, visibility: 'hidden' }} />;
}
