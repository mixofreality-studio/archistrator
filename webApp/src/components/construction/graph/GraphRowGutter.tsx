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
import { GUTTER_PX, gutterLabelFontPx, rowGutterLabels } from './rowGutter';

export function GraphRowGutter({ rows }: { rows: readonly GraphLayoutRow[] }): ReactElement {
  const t = useTokens();
  const transform = useStore((s) => s.transform);
  const canvasHeight = useStore((s) => s.height);
  const labels = rowGutterLabels(rows, transform, canvasHeight);
  const fontPx = gutterLabelFontPx(transform[2]);

  return (
    <Box
      data-testid={UI_IDENTIFIERS.Construction.GRAPH_ROW_GUTTER}
      sx={{
        position: 'absolute',
        left: 0,
        top: 0,
        bottom: 0,
        width: GUTTER_PX,
        // Under xyflow's panels (the zoom controls), over the cards.
        zIndex: 4,
        pointerEvents: 'none',
        overflow: 'hidden',
        // The canvas ground, fading out — a card slid under the gutter while
        // zoomed in stays faintly visible rather than cut off.
        background: `linear-gradient(to right, ${alpha(t.bg, 0.95)} 75%, ${alpha(t.bg, 0)})`,
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
              width: GUTTER_PX - 8,
              top: l.top,
              height: l.height,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'flex-end',
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
                whiteSpace: 'pre-line',
                lineHeight: 1.15,
              }}
            >
              {l.label}
            </Box>
          </Box>
        ))}
    </Box>
  );
}

/** Where a row label node used to be: nothing drawn, but fitView still counts
 *  it, so the fitted canvas keeps the gutter's room clear of cards. */
export function RowSpacerNode(): ReactElement {
  return <Box sx={{ width: GUTTER_W - 22, height: NODE_H, visibility: 'hidden' }} />;
}
