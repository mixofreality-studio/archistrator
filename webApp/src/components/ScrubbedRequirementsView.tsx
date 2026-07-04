/**
 * Scrubbed Requirements as a keyboard-navigable, item-commentable list — NOT flat
 * prose. Each requirement carries a stable `R-0xx` id, so it renders as a row in
 * the shared CommentableList with a per-item "Comment on this item" button that
 * anchors `$.items[id="R-0xx"]` (survives a redraft that reorders items). Bound to
 * adapters.toScrubbedRequirementsView. Safe-empty.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { toScrubbedRequirementsView } from '../contracts/adapters';
import type { ArtifactModelEnvelope } from '../contracts/types';
import { CommentableList } from './comments/CommentableList';
import { scrubbedRequirementAnchor } from './comments/CommentContext';
import { useTokens } from '../utilities/theme/ThemeContext';

export function ScrubbedRequirementsView({
  envelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const items = toScrubbedRequirementsView(envelope);

  if (items.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No scrubbed requirements drafted yet.
      </Box>
    );
  }

  return (
    <CommentableList
      ariaLabel="Scrubbed requirements"
      getAnchor={(item, i) => ({
        kind: 'node',
        label: item.id,
        source: `${item.id} · requirement`,
        jsonPath: scrubbedRequirementAnchor(i, item.id),
      })}
      getKey={(item, i) => item.id || `req-${String(i)}`}
      getLabel={(item) => `requirement ${item.id}`}
      items={items}
      renderItem={(item) => (
        <Box sx={{ display: 'flex', gap: 1.25, alignItems: 'baseline' }}>
          <Typography
            component="span"
            sx={{
              fontFamily: t.mono,
              fontSize: 12,
              fontWeight: 700,
              color: t.accent2,
              flexShrink: 0,
            }}
          >
            {item.id}
          </Typography>
          <Typography component="span" sx={{ color: t.ink, fontFamily: t.body, lineHeight: 1.6 }}>
            {item.statement}
          </Typography>
        </Box>
      )}
    />
  );
}
