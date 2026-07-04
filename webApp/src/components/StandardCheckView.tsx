/**
 * Standard Check as a keyboard-navigable, item-commentable list — NOT flat prose.
 * Each row is one App-C design-guideline outcome (PASS / WAIVED / FAIL + section +
 * guideline + justification), rendered through the shared CommentableList so a
 * reviewer can dispute one row at a time — the natural granularity of a standard
 * check — with a per-item button anchoring `$.items[n]`. Bound to
 * adapters.toStandardCheckView. Safe-empty.
 */
import type { ReactNode } from 'react';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import type { CheckStatus } from '../api/models';
import { toStandardCheckView } from '../api/adapters';
import type { ArtifactModelEnvelope } from '../api/types';
import { CommentableList } from './comments/CommentableList';
import { standardCheckItemAnchor } from './comments/CommentContext';
import { useTokens } from '../theme/ThemeContext';
import type { Tokens } from '../theme/themes';

function statusColor(t: Tokens, status: CheckStatus): { bg: string; fg: string } {
  if (status === 'pass') return { bg: t.committedBg, fg: t.committedFg };
  if (status === 'fail') return { bg: t.awaitingBg, fg: t.dangerFg };
  return { bg: t.awaitingBg, fg: t.awaitingFg }; // waived
}

export function StandardCheckView({
  envelope,
}: {
  envelope: ArtifactModelEnvelope | undefined;
}): ReactNode {
  const t = useTokens();
  const items = toStandardCheckView(envelope);

  if (items.length === 0) {
    return (
      <Box sx={{ py: 6, textAlign: 'center', color: t.muted, fontFamily: t.mono }}>
        No standard check drafted yet.
      </Box>
    );
  }

  return (
    <CommentableList
      ariaLabel="Design standard check"
      getAnchor={(item, i) => ({
        kind: 'node',
        label: `${item.section} · ${item.status}`,
        source: `Standard Check · ${item.section}`,
        jsonPath: standardCheckItemAnchor(i),
      })}
      getKey={(item, i) => `${item.section}-${String(i)}`}
      getLabel={(item) => `${item.section}, ${item.status}`}
      items={items}
      renderItem={(item) => {
        const c = statusColor(t, item.status);
        return (
          <Box>
            <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', mb: 0.25 }}>
              <Box
                component="span"
                sx={{
                  fontFamily: t.mono,
                  fontSize: 10.5,
                  fontWeight: 700,
                  letterSpacing: '0.06em',
                  px: 0.75,
                  py: 0.15,
                  borderRadius: 0.75,
                  bgcolor: c.bg,
                  color: c.fg,
                }}
              >
                {item.status.toUpperCase()}
              </Box>
              <Typography
                component="span"
                sx={{ fontFamily: t.mono, fontSize: 12, fontWeight: 700, color: t.ink }}
              >
                {item.section}
              </Typography>
            </Box>
            <Typography
              sx={{ color: t.ink, fontFamily: t.body, fontSize: '0.92rem', lineHeight: 1.55 }}
            >
              {item.guideline}
            </Typography>
            {item.status === 'waived' && item.justification.length > 0 && (
              <Typography
                sx={{
                  mt: 0.5,
                  color: t.muted,
                  fontFamily: t.body,
                  fontSize: '0.85rem',
                  fontStyle: 'italic',
                }}
              >
                Waived: {item.justification}
              </Typography>
            )}
          </Box>
        );
      }}
    />
  );
}
